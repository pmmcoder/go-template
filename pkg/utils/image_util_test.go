package utils

import (
	"os"
	"testing"

	"github.com/nfnt/resize"
)

func TestScaleImageToTarget(t *testing.T) {
	targetScale := 3.5
	imgInFile := "image_origin.jpeg"
	imgOutFile := "image_origin_3-5.jpeg"
	ai3xFile, _ := os.ReadFile(imgInFile)

	resultBytes, err := ScaleImageToTarget(
		targetScale,
		ai3xFile,
		resize.Lanczos3, // 推荐Lanczos3，质量最高
	)
	if err != nil {
		panic(err)
	}

	// 3. 输出成品文件
	err = os.WriteFile(imgOutFile, resultBytes, 0644)
	if err != nil {
		panic(err)
	}
}
