package wcs_test

import (
	"fmt"
	"jnlee/wcs"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestIsFileExist(t *testing.T) {
	// dummy := map[string]bool{
	// 	"webcacheserver_test.go":   true,
	// 	"webcacheserver.go":        true,
	// 	"./test_dir/dummyFile.txt": true,
	// 	"not_exist_file":           false,
	// }

	// for key, val := range dummy {
	// 	isExist := webcacheserver.IsFileExist(key)
	// 	if isExist != val {
	// 		fmt.Printf("key = %s\n", key)
	// 		t.Error("WrongResult")
	// 	}
	// }

}

func TestGetSha256(t *testing.T) {
	dummy := map[string]string{
		"http://image.gmarket.co.kr/service_image/2023/10/29/20231029235217222142_0_0.jpg": "aeb27f39f8383c9d97842bcd752a6e205a6a0fc56f241e3bb7d7f264033a832f",
		"http://image.gmarket.co.kr/service_image/2023/11/03/20231103133710577882_0_0.jpg": "81eeec41413027e0305e3a22c01acf0157cdbf7c07e53f4c811aee57c6c770ba",
		"http://global.gmarket.co.kr/StaticData/GlobalCommonRVIRecomGoods.js":              "1d50590adc422d3b335b36b5d086bce522831155f4f055978fbe0bc84b36f128",
		"http://global.gmarket.co.kr/StaticData/GlobalHeaderCommonEnData.js":               "78368cd8124ddee6563faa3ee7fc0947f162894710050e644c0c8ebd77082f06",
	}

	for key, val := range dummy {
		sha256 := wcs.GetSha256(key)
		if sha256 != val {
			fmt.Printf("key = %s\n", key)
			t.Error("WrongResult")
		}
	}
}

func TestGetHashKey(t *testing.T) {
	dummy := map[string]int{
		"http://image.gmarket.co.kr/service_image/2023/10/29/20231029235217222142_0_0.jpg": 197,
		"http://image.gmarket.co.kr/service_image/2023/11/03/20231103133710577882_0_0.jpg": 36,
		"http://global.gmarket.co.kr/StaticData/GlobalCommonRVIRecomGoods.js":              105,
		"http://global.gmarket.co.kr/StaticData/GlobalHeaderCommonEnData.js":               242,
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

func TestGetURI(t *testing.T) {
	dummy := []*wcs.Config{
		&wcs.Config{
			0, true, []string{}, true, true, true, 0,
		},
		&wcs.Config{
			0, true, []string{}, true, false, true, 0,
		},
		&wcs.Config{
			0, true, []string{}, false, true, true, 0,
		},
		&wcs.Config{
			0, true, []string{}, false, false, false, 0,
		},
	}

	url, _ := url.Parse("http://global.gmarket.co.kr?a=1&bb=2&c=3&aaa=4&ba=5")
	req := &http.Request{
		URL:    url,
		Method: "GET",
	}
	for _, val := range dummy {
		fmt.Println(GetURI(req, val))
	}

	url2, _ := url.Parse("http://global.gmarket.co.kr?e=0&a=1&bb&c=2&d")
	req2 := &http.Request{
		URL:    url2,
		Method: "GET",
	}
	for _, val := range dummy {
		fmt.Println(GetURI(req2, val))
	}
}

func GetURI(req *http.Request, config *wcs.Config) string {
	myUrl := req.URL
	host := func() string {
		if len(myUrl.Host) == 0 {
			return req.Host
		} else {
			return myUrl.Host
		}
	}()

	switch {
	case len(myUrl.Query()) == 0 || config.QueryIgnoreEnabled:
		return req.Method + host + myUrl.Path
	case config.QuerySortingEnabled:
		var keys []string
		for key := range myUrl.Query() {
			keys = append(keys, key)
		}
		sortedQuery := url.Values{}
		for _, key := range keys {
			qs := myUrl.Query()[key]
			for _, value := range qs {
				if len(value) != 0 {
					sortedQuery.Add(key, value)
				}
			}

		}
		return req.Method + host + myUrl.Path + "?" + sortedQuery.Encode()
	default:
		queries := strings.Split(myUrl.RawQuery, "&")
		var result []string
		for _, query := range queries {
			parts := strings.Split(query, "=")
			if len(parts) == 2 && parts[1] != "" {
				result = append(result, fmt.Sprintf("%s=%s", parts[0], parts[1]))
			}
		}
		return req.Method + host + myUrl.Path + "?" + strings.Join(result, "&")
	}
}

var (
	a int
	b int
	c int
)

type myStruct struct {
	aa int
	bb int
	cc int
}

func BenchmarkGlobal(ttt *testing.B) {
	a = 0
	b = 0
	c = 0

	for i := 0; i < ttt.N; i++ {
		// a += 1
		// b += 1
		// c += 1
		if a > 500000 {
			a -= 1
		} else {
			a += 1
		}
	}
}

func BenchmarkStruct(ttt *testing.B) {
	m := myStruct{0, 0, 0}
	for i := 0; i < ttt.N; i++ {
		if m.aa > 500000 {
			m.aa -= 1
		} else {
			m.aa += 1
		}
		// m.bb += 1
		// m.cc += 1
	}
}
