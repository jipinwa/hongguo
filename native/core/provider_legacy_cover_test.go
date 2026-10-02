package core

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"testing"
)

func TestLegacyCoverAddressesAndSyntheticPayloads(t *testing.T) {
	var encoded bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 4, 4))
	picture.Set(0, 0, color.RGBA{R: 32, G: 64, B: 128, A: 255})
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	plain := encoded.Bytes()
	xor := append([]byte{}, plain...)
	key := []byte("2019ysapp7527")
	for index := 0; index < min(100, len(xor)); index++ {
		xor[index] ^= key[index%len(key)]
	}
	padded := pkcs7Pad(append([]byte{}, plain...), aes.BlockSize)
	encrypted := make([]byte, len(padded))
	block, _ := aes.NewCipher([]byte("f5d965df75336270"))
	cipher.NewCBCEncrypter(block, []byte("97b60394abc2fbe1")).CryptBlocks(encrypted, padded)
	salted := append([]byte("Salted__12345678"), encrypted...)
	for _, payload := range [][]byte{plain, xor, encrypted, salted} {
		if !bytes.Equal(nativeDecodeCover(payload), plain) {
			t.Fatal("a supported synthetic cover encoding was not decoded")
		}
	}
	for _, sample := range []struct{ input, address, referer string }{
		{defaultCDNURL + "/images/synthetic.png?sig=a%2Fb", "https://zzzznnn.lkkwip.cn/images/synthetic.png?sig=a%2Fb", legacyFrontendURL + "/"},
		{defaultCDNURL + "/upload_01/synthetic.png?sig=a%2Fb", "https://pic.zdmhyg.cn/upload_01/synthetic.png?sig=a%2Fb", huangguoAIBaseURL + "/"},
		{"https://new-covers.example.test/synthetic.png?sig=a%2Fb", "https://new-covers.example.test/synthetic.png?sig=a%2Fb", legacyFrontendURL + "/"},
	} {
		t.Run(sample.address, func(t *testing.T) {
			engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != sample.address || request.Header.Get("Referer") != sample.referer || request.Header.Get("Sec-Fetch-Dest") != "image" {
					t.Error("cover route, signature or headers changed", request.URL, request.Header.Get("Referer"))
				}
				return sourceFixtureResponse(request, 200, string(xor)), nil
			})
			drama := nativeDrama{ID: "cloudfront:synthetic", Source: sourceCloudFront, Cover: sample.input}
			path, err := engine.covers.load(context.Background(), drama, false)
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(body, plain) {
				t.Fatal("the device cache did not receive the decoded synthetic cover", err)
			}
		})
	}
	if address := legacyCoverURL(map[string]any{"path": "upload/synthetic.png?signature=%2B"}); address != "https://pic.zdmhyg.cn/upload/synthetic.png?signature=%2B" {
		t.Fatal("relative metadata did not use the image CDN", address)
	}
}
