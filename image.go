package docker

import (
	"crypto"
	"math/big"
	"os"
	"regexp"
	"strings"
)

var loadImageRegexp = regexp.MustCompile(`(?m:^##load-image:.*$)`)

// ExtractLoadImage
//
// ##load-image:alpine
func ExtractLoadImage(buildFile string) string {
	content, err := os.ReadFile(buildFile)
	if err != nil {
		return ""
	}

	image := loadImageRegexp.Find(content)
	if len(image) > 0 {
		return strings.TrimSpace(strings.TrimPrefix(string(image), "##load-image:"))
	}
	return ""
}

var b26Alphabet = []byte("abcdefghijkmnopqrstuvwxyz_")

func base26Encode(input []byte) []byte {
	var result []byte
	x := big.NewInt(0).SetBytes(input)
	base := big.NewInt(int64(len(b26Alphabet)))
	zero := big.NewInt(0)
	mod := &big.Int{}
	for x.Cmp(zero) != 0 {
		x.DivMod(x, base, mod) // 对x取余数
		result = append(result, b26Alphabet[mod.Int64()])
	}
	return result
}

func GetImageName(prefix, id string) string {
	if len(id) > 0 {
		bytes := crypto.MD5.New().Sum([]byte(id))
		return prefix + "." + string(base26Encode(bytes[0:10]))
	}

	return ""
}
