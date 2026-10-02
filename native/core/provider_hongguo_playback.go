package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const hongguoPlaybackAPI = "https://djapi.999888456.xyz/api/hongguo/play"

var hongguoNumericID = regexp.MustCompile(`^[0-9]{1,32}$`)

var hongguoQualityNumber = regexp.MustCompile(`[0-9]+`)

type hongguoPlaybackReference struct {
	ContentType   int    `json:"content_type"`
	FromVideoID   string `json:"from_video_id"`
	SeriesID      string `json:"series_id"`
	VideoID       string `json:"vid"`
	VideoPlatform int    `json:"video_platform"`
}

type hongguoPlaybackResponse struct {
	Parse   json.RawMessage `json:"parse"`
	JX      json.RawMessage `json:"jx"`
	KeyURLs []struct {
		Name   string `json:"name"`
		URL    string `json:"src"`
		KeyID  string `json:"kid"`
		SpadeA string `json:"spade_a"`
	} `json:"key_urls"`
}

func (d *Downloader) resolveHongguoPlaybackAPI(ctx context.Context, seriesID, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(seriesID) || !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("红果播放请求缺少有效的剧集 ID")
	}
	reference, err := json.Marshal(hongguoPlaybackReference{ContentType: 1004, SeriesID: seriesID, VideoID: videoID, VideoPlatform: 3})
	if err != nil {
		return providerMedia{}, err
	}
	query := url.Values{"id": {base64.StdEncoding.EncodeToString(reference)}}
	body, err := d.fetchProviderText(ctx, hongguoPlaybackAPI+"?"+query.Encode(), hongguoBaseURL+"/")
	if err != nil {
		return providerMedia{}, err
	}
	decoded, err := decodeHongguoPlaybackResponse(body)
	if err != nil {
		return providerMedia{}, err
	}
	var response hongguoPlaybackResponse
	if err := json.Unmarshal(decoded, &response); err != nil {
		return providerMedia{}, errors.New("红果备用播放接口返回了无效数据")
	}
	for _, flag := range []json.RawMessage{response.Parse, response.JX} {
		switch strings.TrimSpace(string(flag)) {
		case "", "null", "false", "0", `"0"`, `""`:
		default:
			return providerMedia{}, errors.New("红果备用播放接口没有返回直接媒体地址")
		}
	}
	var selected providerMedia
	bestQuality := -1
	var keyErr error
	var variants []providerMedia
	for _, option := range response.KeyURLs {
		mediaURL := strings.TrimSpace(option.URL)
		if len(mediaURL) > 8192 || !isProviderHTTPMediaURL(mediaURL) {
			continue
		}
		keyID, err := hex.DecodeString(strings.TrimSpace(option.KeyID))
		if err != nil || len(keyID) != aes.BlockSize {
			keyErr = errors.New("红果媒体密钥标识无效")
			continue
		}
		key, err := hongguoContentKey(option.SpadeA)
		if err != nil {
			keyErr = err
			continue
		}
		quality, _ := strconv.Atoi(hongguoQualityNumber.FindString(option.Name))
		media := providerMedia{URL: mediaURL, Referer: "https://novel.snssdk.com/", CENCKey: key, Quality: quality}
		variants = append(variants, media)
		if selected.URL == "" || quality > bestQuality {
			selected = media
			bestQuality = quality
		}
	}
	if selected.URL != "" {
		selected.Variants = variants
		return selected, nil
	}
	if keyErr != nil {
		return providerMedia{}, keyErr
	}
	return providerMedia{}, errors.New("红果备用播放接口未返回该集可用的媒体和密钥，请稍后重试或确认该集是否仍可访问")
}

func decodeHongguoPlaybackResponse(body string) ([]byte, error) {
	text := strings.TrimSpace(body)
	if !strings.HasPrefix(text, "v2.") {
		return []byte(text), nil
	}
	parts := strings.SplitN(text, ".", 3)
	if len(parts) != 3 || len(parts[1]) <= 4 || len(parts[1]) > 1028 {
		return nil, errors.New("红果备用接口响应密钥无效")
	}
	encoded, err := hex.DecodeString(parts[1][4:])
	if err != nil || len(encoded) < 32 {
		return nil, errors.New("红果备用接口响应密钥无效")
	}
	mask := [...]byte{104, 64, 70, 166, 190, 168, 143, 130, 225, 254, 251, 217, 196, 34, 45, 60, 29, 20, 103, 105}
	material := make([]byte, len(encoded))
	for index, current := range encoded {
		previous := byte(109)
		if index > 0 {
			previous = encoded[index-1]
		}
		slot := index % len(mask)
		salt := mask[slot] ^ byte(90+13*slot) ^ 85
		shifted := byte(int(current) + 215 - 11*index)
		material[index] = previous ^ salt ^ bits.RotateLeft8(shifted, 3)
	}
	ciphertext, err := decodeHongguoBase64(parts[2])
	if err != nil || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("红果备用接口加密响应无效")
	}
	block, err := aes.NewCipher(material[:16])
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, material[16:32]).CryptBlocks(plain, ciphertext)
	unpadded, err := pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return nil, errors.New("红果备用接口响应解密失败")
	}
	return unpadded, nil
}

func decodeHongguoBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.Strict().DecodeString(value)
}

func hongguoContentKey(value string) ([]byte, error) {
	if len(value) > 1024 {
		return nil, errors.New("红果媒体密钥数据过长")
	}
	raw, err := decodeHongguoBase64(value)
	if err != nil || len(raw) < 3 {
		return nil, errors.New("红果媒体密钥编码无效")
	}
	tagLength := int(raw[0]^raw[1]^raw[2]) - 48
	contentLength := len(raw) - tagLength - 1
	if tagLength < 1 || contentLength < 33 || contentLength >= len(raw) {
		return nil, errors.New("红果媒体密钥结构无效")
	}
	seed := raw[len(raw)-tagLength-2] ^ raw[len(raw)-tagLength-1]
	tag := make([]byte, tagLength)
	for index := range tag {
		tag[index] = raw[len(raw)-tagLength+index] ^ seed
	}
	if string(tag) == "app_v2" || string(tag) == "web_v2" {
		return nil, errors.New("红果媒体密钥版本暂不支持")
	}
	decoded := make([]byte, contentLength)
	previousEven, previousOdd := byte(250), byte(85)
	for index, current := range raw[1 : 1+contentLength] {
		previous := previousEven
		if index%2 == 0 {
			previousEven = current
		} else {
			previous = previousOdd
			previousOdd = current
		}
		decoded[index] = byte(int(previous^current) - 21 - bits.OnesCount(uint(index)))
	}
	padding, err := strconv.ParseUint(string(decoded[:1]), 36, 8)
	if err != nil || contentLength-int(padding)-1 != 32 {
		return nil, errors.New("红果媒体密钥内容无效")
	}
	key, err := hex.DecodeString(string(decoded[1:33]))
	if err != nil || len(key) != aes.BlockSize {
		return nil, fmt.Errorf("红果媒体密钥不是有效的 AES-128 密钥")
	}
	return key, nil
}
