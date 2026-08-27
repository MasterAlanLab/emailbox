// 生成一个可直接填入 .env 的 ENCRYPTION_KEY。
package main

import (
	"fmt"
	"os"

	"emailbox/pkg/crypto"
)

func main() {
	key, err := crypto.GenerateKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成密钥失败:", err)
		os.Exit(1)
	}
	fmt.Println("ENCRYPTION_KEY=" + key)
}
