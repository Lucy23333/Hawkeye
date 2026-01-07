package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
)

func main() {
	// 1. 硬编码你的 Key 和地址
	apiKey := "sk-95f17d04e8004196b8afc6e49969ed71" 
	url := "https://api.siliconflow.cn/v1/chat/completions"

	// 2. 构造一个最简单的纯文本请求 (不发图片，排除干扰)
	jsonData := []byte(`{
		"model": "deepseek-ai/DeepSeek-V3",
		"messages": [{"role": "user", "content": "Testing, are you alive?"}],
		"stream": false
	}`)

	// 3. 发送请求
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	
	// 🔥 关键点：注意这里有没有多余的空格
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("❌ 网络错误:", err)
		return
	}
	defer resp.Body.Close()

	// 4. 打印结果
	body, _ := ioutil.ReadAll(resp.Body)
	fmt.Println("状态码:", resp.StatusCode)
	fmt.Println("返回内容:", string(body))
}