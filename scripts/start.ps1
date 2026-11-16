#Requires -Version 5.1

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$rootDir = Split-Path -Parent $PSScriptRoot
$frontendDir = Join-Path $rootDir 'fe'
$backendDir = Join-Path $rootDir 'be'

try {
    # Validate Node.js and use npm.cmd to avoid npm.ps1 policy restrictions.
    $nodeCommand = Get-Command node.exe -ErrorAction SilentlyContinue
    $npmCommand = Get-Command npm.cmd -ErrorAction SilentlyContinue
    if (-not $nodeCommand -or -not $npmCommand) {
        throw 'Node.js or npm was not found. Install Node.js 20.19 or later.'
    }
    $npmBin = $npmCommand.Source

    # Locate Go on PATH, then check the local SDK directory.
    $goCommand = Get-Command go.exe -ErrorAction SilentlyContinue
    $goBin = $null
    if ($goCommand) {
        $goBin = $goCommand.Source
    } else {
        $sdkDir = Join-Path $env:USERPROFILE 'sdk'
        # Search SDK installations only when the directory exists.
        if (Test-Path -LiteralPath $sdkDir -PathType Container) {
            $goBin = Get-ChildItem -LiteralPath $sdkDir -Directory |
                ForEach-Object {
                    $candidate = Join-Path $_.FullName 'bin\go.exe'
                    # Only accept installed SDK executables.
                    if (Test-Path -LiteralPath $candidate -PathType Leaf) {
                        $candidate
                    }
                } | Sort-Object | Select-Object -Last 1
        }
    }
    # Stop startup when no Go installation is available.
    if (-not $goBin) {
        throw 'Go was not found. Install Go 1.22 or later.'
    }

    # Validate the configured port before looking up its listener.
    $port = 18080
    if ($env:PORT) {
        # Reject invalid port values before changing any processes.
        if (-not [int]::TryParse($env:PORT, [ref]$port) -or $port -lt 1 -or $port -gt 65535) {
            throw 'PORT must be an integer between 1 and 65535.'
        }
    }

    # Install frontend dependencies on the first startup.
    if (-not (Test-Path -LiteralPath (Join-Path $frontendDir 'node_modules') -PathType Container)) {
        & $npmBin --prefix $frontendDir ci
        # Abort startup when dependency installation fails.
        if ($LASTEXITCODE -ne 0) {
            throw 'Frontend dependency installation failed.'
        }
    }

    # Stop each process listening on the selected port.
    $portProcessIds = @(Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty OwningProcess -Unique)
    foreach ($processId in $portProcessIds) {
        Write-Host "Stopping process $processId on port ${port}..."
        Stop-Process -Id $processId -Force
    }

    Write-Host 'Building frontend...'
    & $npmBin --prefix $frontendDir run build
    # Abort startup when the frontend build fails.
    if ($LASTEXITCODE -ne 0) {
        throw 'Frontend build failed.'
    }

    Write-Host "Starting server: http://127.0.0.1:$port/"
    $env:PORT = [string]$port
    & $goBin -C $backendDir run ./cmd/server
    # Report backend failures to the batch launcher.
    if ($LASTEXITCODE -ne 0) {
        throw "Server exited with code $LASTEXITCODE."
    }
} catch {
    Write-Error $_ -ErrorAction Continue
    exit 1
}
