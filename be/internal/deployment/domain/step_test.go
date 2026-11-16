package domain

import "testing"

func TestFullDeployStepsMergePackageHandling(t *testing.T) {
	if StepIndex(NewSteps(FullDeploySteps...), "合并 package.json") >= 0 {
		t.Fatal("package.json 处理不应作为独立部署步骤")
	}
	if StepIndex(NewSteps(FullDeploySteps...), "修复 package.json 钩子兼容性") >= 0 {
		t.Fatal("钩子修复不应作为独立部署步骤")
	}
	coverIndex := FullDeployStepIndex("覆盖工程目录")
	if FullDeployStepIndex("合并 package.json") != coverIndex ||
		FullDeployStepIndex("修复 package.json 钩子兼容性") != coverIndex {
		t.Fatal("旧步骤应从覆盖工程目录恢复")
	}
}
