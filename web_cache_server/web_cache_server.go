package web_cache_server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	GZIP        string = "gzip"
	GLOBAL_HOST string = "global.gmarket.co.kr"
	IMAGE_HOST  string = "image.gmarket.co.kr"
	CUSTOM_HOST string = "jn.wcs.co.kr"
	CACHED      string = " (Cached)"
	NOT_CACHED  string = " (Not cached)"
)

var (
	sciList        []*safeCacheItem
	sendCacheNum   = 0
	cachedFileNum  = 0
	hitCount       = 0
	serveHttpCount = 0
	config         = Config{}
	myLogger       *MyLogger
)

type safeCacheItem struct {
	rw    *sync.RWMutex
	ciMap map[string]cacheItem
}

type cacheItem struct {
	dir            string
	expirationTime time.Time
	url            string
}

type MyLogger struct {
	logger *log.Logger
}

type Config struct {
	MaxFileSize           int64    `json:"MaxFileSize"`
	GzipEnabled           bool     `json:"GzipEnabled"`
	CacheExceptions       []string `json:"CacheExceptions"`
	QueryIgnoreEnabled    bool     `json:"QueryIgnoreEnabled"`
	QuerySortingEnabled   bool     `json:"QuerySortingEnabled"`
	ResTimeLoggingEnabled bool     `json:"ResponseTimeLoggingEnabled"`
	CleanupFrequency      int      `json:"CleanupFrequency"`
}

type proxyHandler struct {
	proxy map[string]*httputil.ReverseProxy
}

func init() {
	OpenServer()
}

func OpenServer() {
	//For test
	removeDirForTest()

	loadConfig("./web_cache_server/config.json")

	logFile := openLoggerFile("./web_cache_server/log_file.txt")
	defer logFile.Close()
	myLogger = openLogger(logFile)

	// Set RWMutexs
	for i := 0; i < 255; i++ {
		sci := &safeCacheItem{&sync.RWMutex{}, make(map[string]cacheItem)}
		sciList = append(sciList, sci)
	}

	// Set ReverseProxy
	proxyMap := map[string]*httputil.ReverseProxy{
		GLOBAL_HOST: getReverseProxy("http://global.gmarket.co.kr/"),
		IMAGE_HOST:  getReverseProxy("http://image.gmarket.co.kr/"),
		CUSTOM_HOST: getReverseProxy("http://jn.wcs.co.kr/"),
	}
	pHandler := &proxyHandler{proxyMap}
	http.Handle("/", pHandler)

	// Set logging
	go logPerSec()

	// Cleanup Expired Cache
	go cleanupExpiredCaches()

	// Init Server
	fmt.Println("Init server!")
	err := http.ListenAndServe(":80", nil)
	if err != nil {
		panic(err)
	}
}

// 디렉토리가 새로 만들어지는지 확인하기 위해, 프로그램 시작 시 기존 디렉토리 삭제
func removeDirForTest() {
	os.RemoveAll("./web_cache_server/log_body")
	os.RemoveAll("./web_cache_server/log_image")
	os.Remove("./web_cache_server/log_file.txt")
}

func loadConfig(fName string) {
	configData, err := os.ReadFile(fName)
	if err != nil {
		panic(err)
	}

	c := &Config{}
	err = json.Unmarshal(configData, c)
	if err != nil {
		panic(err)
	}
	config = *c
}

func getReverseProxy(uri string) *httputil.ReverseProxy {
	url, err := url.Parse(uri)
	if err != nil {
		panic(err)
	}
	rp := httputil.NewSingleHostReverseProxy(url)
	rp.ModifyResponse = modifyResponse
	return rp
}

func (ph *proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	serveHttpCount += 1

	reverseProxy, ok := ph.proxy[r.Host]
	if !ok {
		w.WriteHeader(404)
		return
	}

	if r.Host == CUSTOM_HOST && r.URL.Path == "/statuspage" { //TODO : status, purge
		showStatusPage(w)
		return
	}

	if r.Host == CUSTOM_HOST && r.Method == http.MethodDelete && r.URL.Path == "/purge" {
		handlePurge(w, r)
		return
	}

	uri := GetURI(r)
	sci := sciList[GetHashkey(uri)]
	ci, exists := sci.ciMap[GetSha256(uri)]

	sTime := time.Now()
	var isCached string
	if IsFileExist(sci.rw, ci.dir) && exists {
		responseByCacheItem(sci.rw, ci.dir, w, getIsGzipAccepted(r))
		isCached = CACHED
	} else {
		reverseProxy.ServeHTTP(w, r)
		isCached = NOT_CACHED
	}

	if config.ResTimeLoggingEnabled {
		elapsedTime := time.Since(sTime)
		url := "http://" + r.Host + r.URL.Path
		myLogger.LogElapsedTime(url+isCached, elapsedTime)
	}
}

func modifyResponse(resp *http.Response) error {
	url := resp.Request.URL
	uri := GetURI(resp.Request)

	if isCacheException(uri) {
		myLogger.logger.Printf("Cache Exception : %s\n", uri)
		return nil
	}

	if !checkHeaderCacheSave(resp) {
		return nil
	}

	if !checkMethodCacheSave(resp.Request.Method) {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	resp.Body = io.NopCloser(bytes.NewReader(body))

	// Unzip gzip
	var dirName string
	if url.Host == GLOBAL_HOST {
		switch resp.Header.Get("Content-Encoding") {
		case "gzip":
			body = GUnzip(body)
		}
		dirName = "./web_cache_server/log_body"
	} else {
		dirName = "./web_cache_server/log_image"
	}
	defer resp.Body.Close()

	// Check File Size
	if len(body) > int(config.MaxFileSize) {
		myLogger.logger.Printf("File size over. Do not cache : %s (%d bytes)\n", url.String(), len(body))
		return nil
	}

	// Cache File
	cacheFile(body, dirName, resp, url.String())

	return nil
}

func showStatusPage(w http.ResponseWriter) {
	percentage := float64(hitCount) / float64(serveHttpCount) * 100

	var sb strings.Builder
	fmt.Fprintf(&sb, "Hit Count = %d", hitCount)
	fmt.Fprintf(&sb, "\nServeHttpCount = %d", serveHttpCount)
	fmt.Fprintf(&sb, "\n\nHit Percentage = %.2f%%", percentage)
	w.Write([]byte(sb.String()))
}

func handlePurge(w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("pattern")
	compiledPattern, err := regexp.Compile(pattern)
	if err != nil {
		myLogger.logger.Printf("정규 표현식 컴파일 오류: %s (%v)\n", pattern, err)
		w.WriteHeader(400)
		return
	}

	for _, sci := range sciList {
		for sha256, ci := range sci.ciMap {
			if compiledPattern.MatchString(ci.url) {
				removeCacheFile(sci, sha256, ci, "Purge")
			}
		}
	}
}

func IsFileExist(rw *sync.RWMutex, dir string) bool {
	rw.RLock()
	defer rw.RUnlock()

	_, err := os.Stat(dir)
	return !os.IsNotExist(err)
}

func responseByCacheItem(rw *sync.RWMutex, dir string, w http.ResponseWriter, isGzipAccepted bool) {
	rw.RLock()
	fileBody, err := os.ReadFile(dir)
	if err != nil {
		panic(err)
	}
	rw.RUnlock()

	if config.GzipEnabled && isGzipAccepted {
		fileBody = GZip(fileBody)
		w.Header().Set("Content-Encoding", "gzip")
	}

	w.Header().Add("jnlee", "HIT")
	w.Write(fileBody)

	rw.Lock()
	sendCacheNum += 1
	hitCount += 1
	rw.Unlock()
}

func GZip(data []byte) []byte {
	buf := &bytes.Buffer{}
	gzWriter := gzip.NewWriter(buf)
	gzWriter.Write(data)
	gzWriter.Close()
	return buf.Bytes()
}

func GUnzip(data []byte) []byte {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		panic(err)
	}
	defer reader.Close()
	return body
}

func GetURI(req *http.Request) string {
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
			parts := strings.SplitN(query, "=", 2)
			if len(parts) == 2 && parts[1] != "" {
				result = append(result, fmt.Sprintf("%s=%s", parts[0], parts[1]))
			}
		}
		return req.Method + host + myUrl.Path + "?" + strings.Join(result, "&")
	}
}

func isQueryIgnoreEnabled(query url.Values) bool {
	if !config.QueryIgnoreEnabled {
		return false
	}
	return len(query) > 0
}

func isCacheException(url string) bool {
	regexps := make([]*regexp.Regexp, len(config.CacheExceptions))
	for i, pattern := range config.CacheExceptions {
		compiledPattern, err := regexp.Compile(pattern)
		if err != nil {
			myLogger.logger.Printf("정규 표현식 컴파일 오류: %s (%v)\n", pattern, err)
			continue
		}
		regexps[i] = compiledPattern
	}

	for _, r := range regexps {
		if r.MatchString(url) {
			return true
		}
	}
	return false
}

// StatueCode, Cache-Control, Content-Type 확인
func checkHeaderCacheSave(resp *http.Response) bool {
	url := resp.Request.URL

	//Check Status Code
	if resp.StatusCode != http.StatusOK {
		myLogger.logger.Printf("CheckHeader : Status not ok. StatusCode = %d, %s\n", resp.StatusCode, url)
		return false
	}

	//Check Cache Control
	cacheControl := resp.Header.Get("Cache-Control")
	if !IsCacheControlSaveAllowed(cacheControl) {
		myLogger.logger.Printf("CheckHeader : Cache-Control Not Allowed (%s) : %s\n", cacheControl, url)
		return false
	}

	//Check Content Type
	contentType := resp.Header.Get("Content-Type")
	if !IsContentTypeSaveAllowed(contentType) {
		myLogger.logger.Printf("CheckHeader : Cache save not allowd by Content-Type (%s) : %s\n", contentType, url)
		return false
	}

	return true
}

func checkMethodCacheSave(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		return false
	}
}

func IsCacheControlSaveAllowed(cacheControl string) bool {
	notAllowed := []string{"no-store", "no-cache", "proxy-revalidate", "private"}
	for _, n := range notAllowed {
		if strings.Contains(cacheControl, n) {
			return false
		}
	}
	return true
}

func IsContentTypeSaveAllowed(contentType string) bool {
	allowed := []string{"text/", "image/"}
	for _, n := range allowed {
		if strings.HasPrefix(contentType, n) {
			return true
		}
	}
	return false
}

func cacheFile(body []byte, dirName string, resp *http.Response, url string) {
	uri := GetURI(resp.Request)
	sha256 := GetSha256(uri)
	expTime := getExpirationTime(resp.Header.Get("Cache-Control"))

	sci := sciList[GetHashkey(uri)]
	go func() {
		sci.rw.Lock()

		os.MkdirAll(dirName, os.ModePerm)
		os.WriteFile(dirName+"/"+sha256, body, 0644)
		cachedFileNum += 1
		sci.ciMap[sha256] = cacheItem{dirName + "/" + sha256, expTime, url}

		sci.rw.Unlock()
	}()
}

func getExpirationTime(cacheControl string) time.Time {
	var exTime time.Time

	if cacheControl != "" {
		re := regexp.MustCompile(`max-age=(\d+)`)
		matches := re.FindStringSubmatch(cacheControl)
		if len(matches) > 1 {
			maxAgeInt, _ := strconv.Atoi(matches[1])
			cTime := time.Now()
			exTime = cTime.Add(time.Duration(maxAgeInt) * time.Second)
		}
	}
	return exTime
}

func cleanupExpiredCaches() {
	ticker := time.NewTicker(time.Second * time.Duration(config.CleanupFrequency))
	defer ticker.Stop()

	for range ticker.C {
		for _, sci := range sciList {
			for sha256, ci := range sci.ciMap {
				if ci.expirationTime.Before(time.Now()) {
					removeCacheFile(sci, sha256, ci, "Expired")
				}
			}
		}
		myLogger.logger.Printf("Cleanup Expired Items\n")
	}
}

func removeCacheFile(sci *safeCacheItem, sha256 string, ci cacheItem, msg string) {
	dir := ci.dir
	sci.rw.Lock()
	defer sci.rw.Unlock()
	err := os.Remove(dir)
	if err != nil {
		myLogger.logger.Printf("파일 삭제 실패 : %e, 삭제하지 못한 파일 : %s\n", err, dir)
		return
	}
	myLogger.logger.Printf("%s) 캐시가 삭제되었습니다 : %s\n", msg, ci.url)
	delete(sci.ciMap, sha256)
}

func logPerSec() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		myLogger.LogCacheNum(cachedFileNum, sendCacheNum)
	}
}

func GetSha256(uri string) string {
	s := sha256.New()
	s.Write([]byte(uri))
	ss := s.Sum(nil)
	return hex.EncodeToString(ss)
}

func GetHashkey(uri string) int {
	s := sha256.New()
	s.Write([]byte(uri))
	ss := s.Sum(nil)

	sha256Int := 0
	for _, val := range ss {
		sha256Int += int(val)
	}
	return sha256Int % 255
}

func getIsGzipAccepted(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), GZIP) && r.Host != IMAGE_HOST
}

// Generate Logger
func openLoggerFile(fName string) *os.File {
	logFile, err := os.OpenFile(fName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	return logFile
}

func openLogger(f *os.File) *MyLogger {
	logger := &MyLogger{log.New(f, "\n", log.Ldate|log.Ltime)}
	return logger
}

func (mLogger *MyLogger) LogElapsedTime(url string, elapsedTime time.Duration) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Elapsed : %s, %s", url, elapsedTime)
	mLogger.logger.Println(sb.String())
}

func (mLogger *MyLogger) LogCacheNum(mCachedFileNum int, mSendCacheNum int) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Cached File Number = %d, Send cache file number = %d", mCachedFileNum, mSendCacheNum)
	mLogger.logger.Println(sb.String())

	cachedFileNum = 0
	sendCacheNum = 0
}
