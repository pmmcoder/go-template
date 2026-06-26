package utils

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/nfnt/resize"
)

// ScaleImageToTarget 将图缩放到目标倍率
// targetScale: 目标放大倍数
// img: 返回的图片（[]byte）
// interType: 插值算法
func ScaleImageToTarget(targetScale float64, img []byte, interType resize.InterpolationFunction) ([]byte, error) {
	// 解码AI高清图
	imgObj, format, err := image.Decode(bytes.NewReader(img))
	if err != nil {
		return nil, err
	}
	fmt.Println(format)

	// 计算从AI图到目标尺寸的实际缩放倍率
	imgObjW, imgObjH := imgObj.Bounds().Dx(), imgObj.Bounds().Dy()
	targetW := uint(float64(imgObjW) * targetScale)
	targetH := uint(float64(imgObjH) * targetScale)

	// 高质量插值缩放
	newImg := resize.Resize(targetW, targetH, imgObj, interType)

	// 编码为JPG返回字节流
	buf := new(bytes.Buffer)

	switch format {
	case "jpg", "jpeg":
		err = jpeg.Encode(buf, newImg, &jpeg.Options{Quality: 95})
	case "png":
		err = png.Encode(buf, newImg)
	default:
		err = errors.New("unsupported image format")
	}

	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
