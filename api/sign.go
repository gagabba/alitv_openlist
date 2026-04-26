package handler

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

var client = resty.New()

func getSign(apiPath string, t string) string {
	params := GetParams(t)
	key := GenerateKey(t)
	data := fmt.Sprintf("POST-/api%v-%v-%v-%v", apiPath, t, params["d"], key)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func h(charArray []rune, modifier interface{}) string {
	uniqueMap := make(map[rune]bool)
	var uniqueChars []rune
	for _, c := range charArray {
		if !uniqueMap[c] {
			uniqueMap[c] = true
			uniqueChars = append(uniqueChars, c)
		}
	}
	modStr := fmt.Sprintf("%v", modifier)
	if len(modStr) < 7 {
		panic("modifier 字符串长度不足7")
	}
	numPart := modStr[7:]
	numericModifier, err := strconv.Atoi(numPart)
	if err != nil {
		panic(err)
	}
	var builder strings.Builder
	for _, char := range uniqueChars {
		charCode := int(char)
		newCharCode := charCode - (numericModifier % 127) - 1
		newCharCode = abs(newCharCode)
		if newCharCode < 33 {
			newCharCode += 33
		}
		builder.WriteRune(rune(newCharCode))
	}
	return builder.String()
}

func GetParams(t interface{}) map[string]string {
	return map[string]string{
		"akv":     "2.8.1496",
		"apv":     "1.4.1",
		"b":       "vivo",
		"d":       "2c7d30cd7ae5e8017384988393f397c6",
		"m":       "V2329A",
		"n":       "V2329A",
		"mac":     "",
		"wifiMac": "00db00200063",
		"nonce":   "",
		"t":       fmt.Sprintf("%v", t),
	}
}

func GenerateKey(t interface{}) string {
	params := GetParams(t)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var concatenatedParams strings.Builder
	for _, k := range keys {
		if k != "t" {
			concatenatedParams.WriteString(params[k])
		}
	}
	keyArray := []rune(concatenatedParams.String())
	hashedKeyString := h(keyArray, t)

	md5Sum := md5.Sum([]byte(hashedKeyString))
	return hex.EncodeToString(md5Sum[:])
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func getTimestamp() string {
	statusResp, err := client.R().
		Get("https://api.extscreen.com/timestamp")
	if err != nil || statusResp.StatusCode() != 200 {
		return strconv.FormatInt(time.Now().Unix(), 10)
	}
	var statusData map[string]interface{}
	json.Unmarshal(statusResp.Body(), &statusData)
	if statusData["code"].(float64) != 200 {
		return strconv.FormatInt(time.Now().Unix(), 10)
	}
	data := statusData["data"].(map[string]interface{})
	return strconv.FormatInt(int64(data["timestamp"].(float64)), 10)
}

func GenerateRequestInfo(apiPath string, body map[string]interface{}) (map[string]interface{}, error) {
	t := getTimestamp()
	keyStr := GenerateKey(t)
	headers := GetParams(t)
	bodyJsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	bodyJsonStr := string(bodyJsonBytes)
	iv := randomString(16)

	encrypted, err := Encrypt(bodyJsonStr, iv, keyStr)
	if err != nil {
		return nil, err
	}
	encryptedBody := map[string]interface{}{
		"ciphertext": encrypted,
		"iv":         iv,
	}
	headers["Content-Type"] = "application/json"
	headers["sign"] = getSign(apiPath, t)

	return map[string]interface{}{
		"headers": headers,
		"body":    encryptedBody,
		"key":     keyStr,
	}, nil
}
