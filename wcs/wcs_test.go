package wcs_test

import (
	"fmt"
	"jnlee/wcs"
	"jnlee/workerpool"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
)

var (
	MockedConfig = ConfigMock{
		c: wcs.ConfigStruct{
			100000, true, []string{
				"cache-exception",
				"/2016/",
			}, false, true, true, 60, "file",
		},
	}
	// dummyFileData []byte
)

type ConfigMock struct {
	c wcs.ConfigStruct
}

func init() {
	wcs.Config = MockedConfig.c

	// str := "abcdefghij"
	// strR := strings.Repeat(str, 200)
	// dummyFileData = []byte(strR)

	wcs.InitWorkerpool()
	wcs.InitMutexAndRedis()
}

// func TestIsFileExist(t *testing.T) {
// 	dummy := map[string]bool{
// 		"wcs_test.go":              true,
// 		"wcs.go":                   true,
// 		"./test_dir/dummyFile.txt": true,
// 		"not_exist_file":           false,
// 	}

// 	for key, val := range dummy {
// 		isExist := wcs.IsFileExist(key)
// 		if isExist != val {
// 			fmt.Printf("key = %s\n", key)
// 			t.Error("WrongResult")
// 		}
// 	}

// }

func TestGetSha256(t *testing.T) {
	dummy := map[string]string{
		// "http://image.gmarket.co.kr/service_image/2023/10/29/20231029235217222142_0_0.jpg": "aeb27f39f8383c9d97842bcd752a6e205a6a0fc56f241e3bb7d7f264033a832f",
		// "http://image.gmarket.co.kr/service_image/2023/11/03/20231103133710577882_0_0.jpg": "81eeec41413027e0305e3a22c01acf0157cdbf7c07e53f4c811aee57c6c770ba",
		// "http://global.gmarket.co.kr/StaticData/GlobalCommonRVIRecomGoods.js":              "1d50590adc422d3b335b36b5d086bce522831155f4f055978fbe0bc84b36f128",
		// "http://global.gmarket.co.kr/StaticData/GlobalHeaderCommonEnData.js":               "78368cd8124ddee6563faa3ee7fc0947f162894710050e644c0c8ebd77082f06",
		"GETimage.gmarket.co.kr/service_image/2023/10/27/20231027174714148076_0_0.jpg": "",
	}

	for key, val := range dummy {
		sha256 := wcs.GetSha256(key)
		if sha256 != val {
			fmt.Printf("key = %s, sha256 = %s\n", key, sha256)
			t.Error("WrongResult")
		}
	}
}

func TestGetHashKey(t *testing.T) {
	dummy := map[string]int{
		// "http://image.gmarket.co.kr/service_image/2023/10/29/20231029235217222142_0_0.jpg": 197,
		// "http://image.gmarket.co.kr/service_image/2023/11/03/20231103133710577882_0_0.jpg": 36,
		// "http://global.gmarket.co.kr/StaticData/GlobalCommonRVIRecomGoods.js":              105,
		// "http://global.gmarket.co.kr/StaticData/GlobalHeaderCommonEnData.js":               242,
		"GETimage.gmarket.co.kr/service_image/2023/10/27/20231027174714148076_0_0.jpg": 91,
	}

	for key, val := range dummy {
		hk := wcs.GetHashkey(key)
		if hk != val {
			fmt.Printf("key = %s, hk = %d\n", key, hk)
			t.Error("WrongResult")
		}
	}
}

func TestIsCacheSaveAllowed(t *testing.T) {
	dummy := map[string]bool{
		"no-store 123123":        false,
		"public, max-age=604800": true,
		"no-cache 12":            false,
		"12 proxy-revalidate":    false,
		"12 proxy-revalidaaate":  true,
		"private":                false,
	}

	for key, val := range dummy {
		ans := wcs.IsCacheControlSaveAllowed(key)
		if ans != val {
			fmt.Printf("key = %s, ans = %t\n", key, ans)
			t.Error("WrongResult")
		}
	}
}

func TestIsContentTypeSaveAllowed(t *testing.T) {
	dummy := map[string]bool{
		"application/json 12314":        false,
		"text":                          false, // no slash
		"abbbb multipart/form-data 121": false,
		"text/html 12":                  true,
		"anything in here":              false,
		"message/rfc82222":              false,
		"image/img":                     true,
		"image":                         false, // no slash
		"text/*":                        true,
	}

	for key, val := range dummy {
		ans := wcs.IsContentTypeSaveAllowed(key)
		if ans != val {
			fmt.Printf("key = %s, ans = %t\n", key, ans)
			t.Error("WrongResult")
		}
	}
}

func TestGzipDecompress(t *testing.T) {
	dummy := []string{
		"156498798646463249{}84{}\\\\316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef	15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94qwe wqf wfaewjfo15649879864646324984316a5sd4fwe43ae4f6asd4fw4e94af asdfwe awef",
		"wefwq fdfwe wercqecrqewe  qwe",
		"ewqwq wg uhdf weq w sgwegdv wef df sadfwef wsd",
		"whatever!!!!!!!!!",
	}

	for _, val := range dummy {
		b := []byte(val)
		af := wcs.GZip(b)
		be := wcs.GUnzip(af)

		if len(b) != len(be) {
			t.Error("Wrong")
		}
	}
}

func TestIsCacheException(t *testing.T) {
	dummy := map[string]bool{
		"my-url-cache-exception-...": true,
		"my-url-cache-not-...":       false,
		"http://image.gmarket.co.kr/service_image/2019/07/03/20190703151059467460_0_0.jpg": false,
		"http://image.gmarket.co.kr/service_image/2023/11/20/20231120134632642997_0_0.jpg": false,
		"http://image.gmarket.co.kr/service_image/2016/11/20/20231120160901321220_0_0.jpg": true,
	}

	for key, val := range dummy {
		hk := wcs.IsCacheException(key)
		if hk != val {
			t.Error("WrongResult")
		}
	}
}

func TestGetURI(t *testing.T) {
	url, _ := url.Parse("http://global.gmarket.co.kr?a=1&bb=2&c=3&aaa=4&ba=5")
	url2, _ := url.Parse("http://global.gmarket.co.kr?e=0&a=1&bb&c=2&d")
	url3, _ := url.Parse("http://image.gmarket.co.kr/service_image/2023/10/27/20231027174714148076_0_0.jpg")

	dummy := map[*http.Request]string{
		&http.Request{
			URL:    url,
			Method: http.MethodGet,
		}: "GETglobal.gmarket.co.kr?a=1&aaa=4&ba=5&bb=2&c=3",
		&http.Request{
			URL:    url2,
			Method: http.MethodGet,
		}: "GETglobal.gmarket.co.kr?a=1&c=2&e=0",
		&http.Request{
			URL:    url3,
			Method: http.MethodGet,
		}: "GETimage.gmarket.co.kr/service_image/2023/10/27/20231027174714148076_0_0.jpg",
	}

	for key, val := range dummy {
		hk := wcs.GetURI(key)
		fmt.Println(hk)
		if hk != val {
			t.Error("WrongResult")
		}
	}
}

func TestGetExpirationTime(t *testing.T) {
	now := time.Now()
	dummy := map[string]time.Time{
		"private, max-age=3600":                           now.Add(time.Second * 3600),
		"private, max-age=900":                            now.Add(time.Second * 900),
		"public,max-age=1200,stale-while-revalidate=3600": now.Add(time.Second * 1200),
		"no-cache": time.Time{},
	}
	for key, val := range dummy {
		hk := wcs.GetExpirationTime(key)
		if hk.Sub(val) > time.Millisecond {
			t.Error("Wrong")
		}
	}
}

// var (
// 	a int
// 	b int
// 	c int
// )

// type myStruct struct {
// 	aa int
// 	bb int
// 	cc int
// }

// func BenchmarkGlobal(ttt *testing.B) {
// 	a = 0
// 	b = 0
// 	c = 0

// 	for i := 0; i < ttt.N; i++ {
// 		// a += 1
// 		// b += 1
// 		// c += 1
// 		if a > 500000 {
// 			a -= 1
// 		} else {
// 			a += 1
// 		}
// 	}
// }

// func BenchmarkStruct(ttt *testing.B) {
// 	m := myStruct{0, 0, 0}
// 	for i := 0; i < ttt.N; i++ {
// 		if m.aa > 500000 {
// 			m.aa -= 1
// 		} else {
// 			m.aa += 1
// 		}
// 		// m.bb += 1
// 		// m.cc += 1
// 	}
// }

func BenchmarkGoroutine(b *testing.B) {
	b.ReportAllocs()

	var wg sync.WaitGroup

	for i := 0; i < b.N; i++ {

		wg.Add(1)
		go func() {

			// url, _ := url.Parse("http://global.gmarket.co.kr")
			// resp := &http.Response{
			// 	Request: &http.Request{
			// 		URL: url,
			// 	},
			// }
			// wcs.CacheFile(dummyFileData, resp)
			time.Sleep(time.Microsecond)
			// generateRandomString(1)
			wg.Done()
		}()
	}

	wg.Wait()
}

func BenchmarkWorkerpool(b *testing.B) {
	b.ReportAllocs()

	wp := workerpool.NewWorkerPool(255)
	wp.Run()

	var wg sync.WaitGroup

	for i := 0; i < b.N; i++ {
		// url, _ := url.Parse("http://global.gmarket.co.kr")
		// resp := &http.Response{
		// 	Request: &http.Request{
		// 		URL: url,
		// 	},
		// }
		wg.Add(1)
		wp.AddTask(func() {
			// wcs.CacheFile(dummyFileData, resp)
			time.Sleep(time.Microsecond)
			// generateRandomString(1)
			wg.Done()
		})
	}
	wg.Wait()
}
func generateRandomString(length int) string {
	// 무작위 시드 초기화
	rand.Seed(time.Now().UnixNano())

	// 사용할 문자열의 문자들
	charSet := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// 문자열을 담을 변수 초기화
	result := make([]byte, length)

	// 문자열 생성
	for i := 0; i < length; i++ {
		result[i] = charSet[rand.Intn(len(charSet))]
	}

	return string(result)
}
