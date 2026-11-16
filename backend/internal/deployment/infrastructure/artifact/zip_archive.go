package artifact

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"app-deployment-platform/internal/deployment/domain"
)

const (
	PackageManifest = "miaoda-package.json"
	MaxUpload       = 256 << 20
	maxExpanded     = 1 << 30
	maxEntries      = 25000
)

type ZipArchive struct{}

func NewZipArchive() *ZipArchive {
	return &ZipArchive{}
}

func validateFiles(files []*zip.File) error {
	if len(files) == 0 || len(files) > maxEntries {
		return errors.New("安装包的文件数量不受支持")
	}
	seen := map[string]bool{}
	var total uint64
	// 校验路径、文件类型和解压体积。
	for _, file := range files {
		name := strings.TrimSuffix(file.Name, "/")
		if name == "." || name == ".." || name == "" || name != path.Clean(name) ||
			strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:\x00") {
			return errors.New("安装包含有无效的文件路径")
		}
		for _, part := range strings.Split(name, "/") {
			if strings.EqualFold(part, ".git") {
				return errors.New("安装包含有 Git 仓库内部文件")
			}
		}
		normalized := strings.ToLower(name)
		if seen[normalized] || (!file.FileInfo().IsDir() && !file.Mode().IsRegular()) {
			return errors.New("安装包含有重复路径或不支持的文件类型")
		}
		seen[normalized] = true
		if file.UncompressedSize64 > maxExpanded-total {
			return errors.New("安装包解压后超过 1 GB")
		}
		total += file.UncompressedSize64
	}
	return nil
}

func validateLayout(files []*zip.File, kind string) error {
	check := map[string]bool{}
	// 收集 ZIP 根目录结构。
	for _, file := range files {
		root, _, nested := strings.Cut(file.Name, "/")
		if nested || !file.FileInfo().IsDir() {
			check[root] = true
		}
	}
	if kind == "full_stack" {
		for _, name := range []string{"client", "server", "shared", "package.json"} {
			if !check[name] {
				return errors.New("全栈包缺少 " + name)
			}
		}
	}
	return nil
}

func detectType(files []*zip.File) (string, error) {
	check := map[string]bool{}
	// 收集 ZIP 根目录结构。
	for _, file := range files {
		root, _, nested := strings.Cut(file.Name, "/")
		if nested || !file.FileInfo().IsDir() {
			check[strings.ToLower(root)] = true
		}
	}
	switch {
	case check["client"] && check["server"] && check["shared"] && check["package.json"]:
		return "full_stack", nil
	case check["package.json"]:
		return "frontend", nil
	case check["index.html"]:
		return "html", nil
	default:
		return "", errors.New("无法识别应用类型，ZIP 根目录须包含全栈工程、package.json 或 index.html")
	}
}

func (z *ZipArchive) Inspect(filename string) (domain.Manifest, error) {
	var meta domain.Manifest
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return meta, errors.New("无法打开 ZIP 安装包")
	}
	defer reader.Close()
	if err := validateFiles(reader.File); err != nil {
		return meta, err
	}
	entry, err := reader.Open(PackageManifest)
	if err != nil {
		// 无清单时根据工程结构识别应用类型。
		meta.App.Type, err = detectType(reader.File)
		if err != nil {
			return meta, err
		}
		meta.Version = 1
		return meta, validateLayout(reader.File, meta.App.Type)
	}
	defer entry.Close()
	decoder := json.NewDecoder(io.LimitReader(entry, 64<<10))
	if decoder.Decode(&meta) != nil || decoder.Decode(new(any)) != io.EOF ||
		meta.Version != 1 || !domain.AppIDPattern.MatchString(meta.App.ID) ||
		strings.TrimSpace(meta.App.Name) == "" || !domain.SupportedAppType(meta.App.Type) {
		return domain.Manifest{}, errors.New("安装包清单无效或版本不受支持")
	}
	return meta, validateLayout(reader.File, meta.App.Type)
}

func (z *ZipArchive) Create(source, target string, app domain.App) error {
	input, err := zip.OpenReader(source)
	if err != nil {
		return errors.New("妙搭未生成有效的 ZIP")
	}
	defer input.Close()
	if err := validateFiles(input.File); err != nil {
		return err
	}
	if err := validateLayout(input.File, app.Type); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	writer := zip.NewWriter(output)
	// 复制源码并替换平台清单。
	for _, file := range input.File {
		if strings.EqualFold(file.Name, PackageManifest) {
			continue
		}
		if err := writer.Copy(file); err != nil {
			return err
		}
	}
	entry, err := writer.Create(PackageManifest)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(entry).Encode(domain.Manifest{Version: 1, App: app}); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return output.Close()
}

func (z *ZipArchive) Extract(filename, dir string) error {
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := validateFiles(reader.File); err != nil {
		return err
	}
	// 逐项解压经过校验的文件。
	for _, file := range reader.File {
		if file.Name == PackageManifest {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(file.Name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if err := extractFile(file, target); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(file *zip.File, target string) error {
	input, err := file.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	mode := os.FileMode(0600) | (file.Mode().Perm() & 0100)
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	count, copyErr := io.Copy(output, io.LimitReader(input, int64(file.UncompressedSize64)+1))
	closeErr := output.Close()
	if copyErr != nil || count != int64(file.UncompressedSize64) {
		return errors.New("安装包文件内容损坏")
	}
	return closeErr
}
