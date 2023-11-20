package wcs

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/go-redis/redis"
)

const (
	GZIP        string = "gzip"
	GLOBAL_HOST string = "global.gmarket.co.kr"
	IMAGE_HOST  string = "image.gmarket.co.kr"
	CUSTOM_HOST string = "jn.wcs.co.kr"
	CACHED      string = " (Cached)"
	NOT_CACHED  string = " (Not cached)"
	CONFIG_PATH string = "./wcs/config.json"
)

var (
	safeClientList      []*safeClient
	sendCacheNum        = 0
	cachedFileNum       = 0
	globalHitCount      = 0
	globalRequestsCount = 0
	imageHitCount       = 0
	imageRequestsCount  = 0
	config              = Config{}
	myLogger            *MyLogger
)

type safeClient struct {
	rwMutex *sync.RWMutex
	client  *redis.Client
}

type CacheItem struct {
	Header         http.Header
	Body           []byte
	URL            string
	ExpirationTime time.Time
	CachedTime     time.Time
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

type HTMLData struct {
	HitData    []htmlHitData
	ConfigData []htmlConfigData
}
type htmlHitData struct {
	Title    string
	Hit      int
	Requests int
	Percent  float64
}
type htmlConfigData struct {
	Name  string
	Value string
}

func init() {
	OpenServer()
}

func OpenServer() {
	//For test
	removeDirForTest()

	loadConfig()

	initClientList()

	logFile := openLoggerFile("./wcs/log_file.txt")
	defer logFile.Close()
	myLogger = generateLogger(logFile)

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
	os.RemoveAll("./wcs/log_body")
	os.RemoveAll("./wcs/log_image")
	os.Remove("./wcs/log_file.txt")
}

func loadConfig() {
	configData, err := os.ReadFile(CONFIG_PATH)
	if err != nil {
		panic(err)
	}

	newConfig := &Config{}
	err = json.Unmarshal(configData, newConfig)
	if err != nil {
		panic(err)
	}
	config = *newConfig
}

func initClientList() {
	for i := 0; i < 255; i++ {
		port := strconv.Itoa(6379 + i)
		redisClient := redis.NewClient(&redis.Options{
			Addr:     "192.168.0.88:" + port,
			Password: "",
			DB:       i,
		})
		safeClient := &safeClient{&sync.RWMutex{}, redisClient}
		safeClientList = append(safeClientList, safeClient)
	}
}

func getReverseProxy(uri string) *httputil.ReverseProxy {
	url, err := url.Parse(uri)
	if err != nil {
		panic(err)
	}
	reverseProxy := httputil.NewSingleHostReverseProxy(url)
	reverseProxy.ModifyResponse = modifyResponse
	return reverseProxy
}

func (ph *proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reverseProxy, ok := ph.proxy[r.Host]
	if !ok {
		w.WriteHeader(404)
		return
	}

	if r.Host == CUSTOM_HOST && r.URL.Path == "/statuspage" {
		showStatusPage(w)
		return
	}

	if r.Host == CUSTOM_HOST && r.Method == http.MethodDelete && r.URL.Path == "/purge" {
		handlePurge(w, r)
		return
	}

	increseRequestsCount(r.Host)

	uri := GetURI(r)
	sha256 := GetSha256(uri)
	safeClient := safeClientList[GetHashkey(uri)]
	safeClient.rwMutex.RLock()
	exists, err := safeClient.client.Exists(sha256).Result()
	if err == redis.Nil {
		fmt.Println("Redis nil")
	} else if err != nil {
		fmt.Printf("err = %s\nhashKey = %d\nuri=%s\n", err, GetHashkey(uri), uri)
		panic(err)
	}
	safeClient.rwMutex.RUnlock()

	startTime := time.Now()
	var isCached string
	if exists == 1 { //IsFileExist(safeClient.rwMutex, ci.Dir) &&
		responseByCacheItem(safeClient, sha256, w, r)
		isCached = CACHED
	} else {
		reverseProxy.ServeHTTP(w, r)
		isCached = NOT_CACHED
	}

	if config.ResTimeLoggingEnabled {
		elapsedTime := time.Since(startTime)
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
	if url.Host == GLOBAL_HOST && resp.Header.Get("Content-Encoding") == GZIP {
		body = GUnzip(body)
	}
	defer resp.Body.Close()

	// Check File Size
	if len(body) > int(config.MaxFileSize) {
		myLogger.logger.Printf("File size over. Do not cache : %s (%d bytes)\n", url.String(), len(body))
		return nil
	}

	// Cache File
	cacheFile(body, resp)

	return nil
}

func increseRequestsCount(host string) {
	switch host {
	case GLOBAL_HOST:
		globalRequestsCount += 1
	case IMAGE_HOST:
		imageRequestsCount += 1
	}
}

func increseHitCount(host string) {
	switch host {
	case GLOBAL_HOST:
		globalHitCount += 1
	case IMAGE_HOST:
		imageHitCount += 1
	}
}

func showStatusPage(w http.ResponseWriter) {
	getPercent := func(hit int, req int) float64 {
		if hit == 0 {
			return 0
		}
		perFloat := float64(hit) / float64(req) * 100
		return math.Round(perFloat*100) / 100
	}

	globalPercent := getPercent(globalHitCount, globalRequestsCount)
	imagePercent := getPercent(imageHitCount, imageRequestsCount)
	totalPercent := getPercent(globalHitCount+imageHitCount, globalRequestsCount+imageRequestsCount)

	htmlDataList := []htmlHitData{
		{"Global", globalHitCount, globalRequestsCount, globalPercent},
		{"Image", imageHitCount, imageRequestsCount, imagePercent},
		{"Total", globalHitCount + imageHitCount, globalRequestsCount + imageRequestsCount, totalPercent},
	}

	configDataList := []htmlConfigData{}
	configData := htmlConfigData{}
	configDataMap := getConfigDatas()
	for key, value := range configDataMap {
		val := fmt.Sprintf("%v", value)
		configData.Name = key
		configData.Value = val
		configDataList = append(configDataList, configData)
	}

	tmpl, err := template.ParseFiles("./wcs/status-page.html")
	if err != nil {
		panic(err)
	}

	htmlData := HTMLData{htmlDataList, configDataList}
	err = tmpl.Execute(w, htmlData)
	if err != nil {
		panic(err)
	}
}

func getConfigDatas() map[string]interface{} {
	file, err := os.ReadFile(CONFIG_PATH)
	if err != nil {
		panic(err)
	}

	var data map[string]interface{}
	err = json.Unmarshal(file, &data)
	if err != nil {
		panic(err)
	}

	return data
}

func handlePurge(w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("pattern")
	compiledPattern, err := regexp.Compile(pattern)
	if err != nil {
		myLogger.logger.Printf("정규 표현식 컴파일 오류: %s (%v)\n", pattern, err)
		w.WriteHeader(400)
		return
	}

	for _, safeClient := range safeClientList {
		safeClient.rwMutex.Lock()
		client := safeClient.client
		keys, err := client.Keys("*").Result()
		if err != nil {
			panic(err)
		}

		for _, sha256 := range keys {
			cacheItem := getCacheItem(client, sha256)
			if compiledPattern.MatchString(cacheItem.URL) {
				removeCacheFile(client, sha256, cacheItem, "Purge")
			}
		}
		safeClient.rwMutex.Unlock()
	}
}

func responseByCacheItem(safeClient *safeClient, sha256 string, w http.ResponseWriter, r *http.Request) {
	rwMutex := safeClient.rwMutex
	rwMutex.RLock()
	cacheItem := getCacheItem(safeClient.client, sha256)
	rwMutex.RUnlock()
	fileBody := cacheItem.Body

	if config.GzipEnabled && getIsGzipAccepted(r) {
		fileBody = GZip(fileBody)
		w.Header().Set("Content-Encoding", GZIP)
	}

	setHeaderFromCache := func(headerKey string) {
		w.Header().Set(headerKey, cacheItem.Header.Get(headerKey))
	}
	setHeaderFromCache("Cache-Control")
	setHeaderFromCache("Etag")

	w.Header().Set("Age", strconv.Itoa(int(time.Since(cacheItem.CachedTime).Seconds())))
	w.Header().Add("jnlee", "HIT")
	w.Write(fileBody)

	rwMutex.Lock()
	sendCacheNum += 1
	increseHitCount(r.Host)
	rwMutex.Unlock()
}

func getCacheItem(client *redis.Client, sha256 string) CacheItem {
	ciJSON, err := client.Get(sha256).Result()
	if err != nil {
		panic(err)
	}
	var ci CacheItem
	json.Unmarshal([]byte(ciJSON), &ci)
	return ci
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
			queries := myUrl.Query()[key]
			for _, value := range queries {
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

func cacheFile(body []byte, resp *http.Response) {
	uri := GetURI(resp.Request)
	sha256 := GetSha256(uri)
	expTime := getExpirationTime(resp.Header.Get("Cache-Control"))

	safeClient := safeClientList[GetHashkey(uri)]
	go func() {
		safeClient.rwMutex.Lock()

		ci := CacheItem{
			Header:         resp.Header,
			Body:           body,
			URL:            resp.Request.URL.String(),
			ExpirationTime: expTime,
			CachedTime:     time.Now(),
		}
		ciJSON, _ := json.Marshal(ci)
		safeClient.client.Set(sha256, ciJSON, 0).Err()
		cachedFileNum += 1

		safeClient.rwMutex.Unlock()
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
		for _, safeClient := range safeClientList {
			safeClient.rwMutex.Lock()
			client := safeClient.client
			keys, err := client.Keys("*").Result()
			if err != nil {
				panic(err)
			}

			for _, sha256 := range keys {
				cacheItem := getCacheItem(client, sha256)
				if cacheItem.ExpirationTime.Before(time.Now()) {
					removeCacheFile(client, sha256, cacheItem, "Expired")
				}
			}
			safeClient.rwMutex.Unlock()
		}
		myLogger.logger.Printf("Cleanup Expired Items\n")
	}
}

func removeCacheFile(client *redis.Client, sha256 string, ci CacheItem, logMsg string) {
	_, err := client.Del(sha256).Result() //_ : 지워진 값 개수
	if err != nil {
		panic(err)
	}
	myLogger.logger.Printf("%s) 캐시가 삭제되었습니다 : %s\n", logMsg, ci.URL)
}

func logPerSec() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		myLogger.LogCacheNum(cachedFileNum, sendCacheNum)
	}
}

func GetSha256(uri string) string {
	newSha := sha256.New()
	newSha.Write([]byte(uri))
	return hex.EncodeToString(newSha.Sum(nil))
}

func GetHashkey(uri string) int {
	newSha := sha256.New()
	newSha.Write([]byte(uri))

	sha256Int := 0
	for _, v := range newSha.Sum(nil) {
		sha256Int += int(v)
	}
	return sha256Int % 255
}

func getIsGzipAccepted(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), GZIP) && r.Host != IMAGE_HOST
}

func openLoggerFile(fName string) *os.File {
	logFile, err := os.OpenFile(fName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	return logFile
}

func generateLogger(f *os.File) *MyLogger {
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
