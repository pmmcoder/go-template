package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 全局带超时客户端，避免连接卡死
var client = &http.Client{
	Timeout: 10 * time.Second,
}

func main() {
	url := "https://genai.holopix.cn/uploads/20260617/1781670560750-uTqgAoHPUDEipacwFlV-c.jpeg"

	// 构造请求添加UA，防止403防盗链
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		fmt.Printf("创建请求失败: %v\n", err)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/120.0.0.0 Safari/537.36")

	// 发起请求
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("请求图片失败: %v\n", err)
		return // 解析失败直接return，不会走到resp操作
	}
	// 仅resp非空才延迟关闭
	defer resp.Body.Close()

	// 校验HTTP状态码
	if resp.StatusCode != http.StatusOK {
		err = errors.New("请求失败，状态码：" + resp.Status)
		fmt.Println(err)
		return
	}

	// 读取图片字节
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("读取图片流失败: %v\n", err)
		return
	}

	// 计算sha256
	sum := sha256.Sum256(data)
	hashHex := hex.EncodeToString(sum[:])
	fmt.Println("图片sha256:", hashHex)
}
