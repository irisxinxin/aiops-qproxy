#!/bin/bash
# 测试 isConnectionError 函数是否正确识别错误

echo "🧪 测试 isConnectionError 函数..."

# 创建一个简单的 Go 测试程序
cat > /tmp/test_connection_error.go << 'EOF'
package main

import (
	"errors"
	"fmt"
	"strings"
)

func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	
	errStr := err.Error()
	connectionErrors := []string{
		"broken pipe",
		"connection reset",
		"connection refused",
		"network is unreachable",
		"i/o timeout",
		"use of closed network connection",
	}
	
	for _, connErr := range connectionErrors {
		if strings.Contains(errStr, connErr) {
			return true
		}
	}
	
	return false
}

func main() {
	testErrors := []error{
		errors.New("write tcp 127.0.0.1:32806->127.0.0.1:7682: write: broken pipe"),
		errors.New("connection reset by peer"),
		errors.New("connection refused"),
		errors.New("network is unreachable"),
		errors.New("i/o timeout"),
		errors.New("use of closed network connection"),
		errors.New("some other error"),
		errors.New("temporary failure"),
	}
	
	for _, err := range testErrors {
		result := isConnectionError(err)
		fmt.Printf("错误: %s\n", err.Error())
		fmt.Printf("是否为连接错误: %v\n", result)
		fmt.Println("---")
	}
}
EOF

# 运行测试
go run /tmp/test_connection_error.go

# 清理
rm -f /tmp/test_connection_error.go
