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
	"sort"
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
	WCS_PATH    string = "./wcs/"
)

var (
	rwMutextList []*sync.RWMutex
	redisClient  *redis.Client //key = hashKey, field = sha256
	config       = Config{}
	myLogger     *MyLogger
	countData    = countDatas{&sync.RWMutex{}, 0, 0, 0, 0, 0, 0}
)

type countDatas struct {
	rwMutex    *sync.RWMutex
	sendCache  int
	cachedFile int
	gHit       int
	gRequest   int
	iHit       int
	iRequest   int
}

type CacheItem struct {
	Header         http.Header
	Body           []byte
	URL            string
	Host           string
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
	CacheData  htmlCacheData
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
type htmlCacheData struct {
	ImageData       []string
	ImageDataCount  int
	Images1         []string
	Images2         []string
	Images3         []string
	GlobalData      []string
	GlobalDataCount int
}

func init() {}

func OpenServer() {
	//For test
	initRedisClient()

	loadConfig()

	initMutexList()

	removeDirForTest()

	logFile := openLoggerFile(WCS_PATH + "log_file.txt")
	defer logFile.Close()
	myLogger = generateLogger(logFile)

	// Set ReverseProxy
	proxyMap := map[string]*httputil.ReverseProxy{
		GLOBAL_HOST: getReverseProxy(GLOBAL_HOST),
		IMAGE_HOST:  getReverseProxy(IMAGE_HOST),
		CUSTOM_HOST: getReverseProxy(CUSTOM_HOST),
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
	os.Remove(WCS_PATH + "log_file.txt")
	redisClient.FlushDB().Result()
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

func initMutexList() {
	for i := 0; i < 255; i++ {
		rw := &sync.RWMutex{}
		rwMutextList = append(rwMutextList, rw)
	}
}

func initRedisClient() {
	redisClient = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
}

func getReverseProxy(host string) *httputil.ReverseProxy {
	url, err := url.Parse("http://" + host)
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
	hashKey := GetHashkey(uri)

	rwMutextList[hashKey].RLock()
	exists, err := redisClient.HExists(strconv.Itoa(hashKey), sha256).Result()
	if err != nil {
		panic(err)
	}
	rwMutextList[hashKey].RUnlock()

	startTime := time.Now()
	var isCached string
	if exists {
		responseByCacheItem(hashKey, sha256, w, r)
		isCached = CACHED
	} else {
		reverseProxy.ServeHTTP(w, r)
		isCached = NOT_CACHED
	}

	if config.ResTimeLoggingEnabled {
		elapsedTime := time.Since(startTime)
		myLogger.LogElapsedTime(r.Host+r.URL.Path+isCached, elapsedTime)
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
	countData.rwMutex.Lock()
	countData.sendCache += 1
	switch host {
	case GLOBAL_HOST:
		countData.gRequest += 1
	case IMAGE_HOST:
		countData.iRequest += 1
	}
	countData.rwMutex.Unlock()
}

func increseHitCount(host string) {
	countData.rwMutex.Lock()
	countData.sendCache += 1
	switch host {
	case GLOBAL_HOST:
		countData.gHit += 1
	case IMAGE_HOST:
		countData.iHit += 1
	}
	countData.rwMutex.Unlock()
}

func showStatusPage(w http.ResponseWriter) {
	getPercent := func(hit int, req int) float64 {
		if hit == 0 {
			return 0
		}
		perFloat := float64(hit) / float64(req) * 100
		return math.Round(perFloat*100) / 100
	}

	countData.rwMutex.RLock()
	gPercent := getPercent(countData.gHit, countData.gRequest)
	iPercent := getPercent(countData.iHit, countData.iRequest)
	tPercent := getPercent(countData.gHit+countData.iHit, countData.gRequest+countData.iRequest)

	htmlDataList := []htmlHitData{
		{"Global", countData.gHit, countData.gRequest, gPercent},
		{"Image", countData.iHit, countData.iRequest, iPercent},
		{"Total", countData.gHit + countData.iHit, countData.gRequest + countData.iRequest, tPercent},
	}
	countData.rwMutex.RUnlock()

	configDataList := []htmlConfigData{}
	configData := htmlConfigData{}
	for key, value := range getConfigDatas() {
		val := fmt.Sprintf("%v", value)
		configData.Name = key
		configData.Value = val
		configDataList = append(configDataList, configData)
	}
	sort.Slice(configDataList, func(i, j int) bool {
		return configDataList[i].Name < configDataList[j].Name
	})

	tmpl, err := template.ParseFiles(WCS_PATH + "status-page.html")
	if err != nil {
		panic(err)
	}

	htmlData := HTMLData{htmlDataList, configDataList, getCachedData()}
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

	for hashKey, rwMutex := range rwMutextList {
		rwMutex.Lock()
		result, err := redisClient.HGetAll(strconv.Itoa(hashKey)).Result()
		if err != nil {
			panic(err)
		}

		for sha256, _ := range result {
			cacheItem := getCacheItem(hashKey, sha256)
			if compiledPattern.MatchString(cacheItem.URL) {
				removeCacheFile(hashKey, sha256, cacheItem.URL, "Purge")
			}
		}
		rwMutex.Unlock()
	}
}

func getCachedData() (cachedData htmlCacheData) {
	for hashKey, rwMutex := range rwMutextList {
		rwMutex.Lock()
		result, err := redisClient.HGetAll(strconv.Itoa(hashKey)).Result()
		if err != nil {
			panic(err)
		}

		for sha256, _ := range result {
			cacheItem := getCacheItem(hashKey, sha256)

			switch cacheItem.Host {
			case IMAGE_HOST:
				cachedData.ImageData = append(cachedData.ImageData, cacheItem.URL)
				cachedData.ImageDataCount += 1
			default:
				cachedData.GlobalData = append(cachedData.GlobalData, cacheItem.URL)
				cachedData.GlobalDataCount += 1
			}
		}
		rwMutex.Unlock()
	}
	length := len(cachedData.ImageData)
	length /= 3
	cachedData.Images1 = cachedData.ImageData[:length]
	cachedData.Images2 = cachedData.ImageData[length : length*2]
	cachedData.Images3 = cachedData.ImageData[length*2:]

	return cachedData
}

func responseByCacheItem(hashKey int, sha256 string, w http.ResponseWriter, r *http.Request) {
	rwMutextList[hashKey].RLock()
	cacheItem := getCacheItem(hashKey, sha256)
	rwMutextList[hashKey].RUnlock()
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

	increseHitCount(r.Host)
}

func getCacheItem(hashKey int, sha256 string) CacheItem {
	ciJSON, err := redisClient.HGet(strconv.Itoa(hashKey), sha256).Result()
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

// StatueCode, Method, Cache-Control, Content-Type 확인
func checkHeaderCacheSave(resp *http.Response) bool {
	url := resp.Request.URL

	//Check Status Code
	if resp.StatusCode != http.StatusOK {
		myLogger.logger.Printf("CheckHeader : Status not ok. StatusCode = %d, %s\n", resp.StatusCode, url)
		return false
	}

	//Checkc Method
	if resp.Request.Method != http.MethodGet && resp.Request.Method != http.MethodHead {
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
	hashKey := GetHashkey(uri)

	go func() {
		ci := CacheItem{
			Header:         resp.Header,
			Body:           body,
			URL:            resp.Request.URL.String(),
			Host:           resp.Request.Host,
			ExpirationTime: getExpirationTime(resp.Header.Get("Cache-Control")),
			CachedTime:     time.Now(),
		}
		ciJSON, _ := json.Marshal(ci)

		rwMutextList[hashKey].Lock()
		err := redisClient.HSet(strconv.Itoa(hashKey), sha256, ciJSON).Err()
		if err != nil {
			panic(err)
		}
		rwMutextList[hashKey].Unlock()

		countData.rwMutex.Lock()
		countData.cachedFile += 1
		countData.rwMutex.Unlock()
	}()
}

func getExpirationTime(cacheControl string) time.Time {
	var exTime time.Time

	if cacheControl != "" {
		matches := regexp.MustCompile(`max-age=(\d+)`).FindStringSubmatch(cacheControl)
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
		for hashKey, rwMutex := range rwMutextList {
			rwMutex.Lock()
			result, err := redisClient.HGetAll(strconv.Itoa(hashKey)).Result()
			if err != nil {
				panic(err)
			}

			for sha256, _ := range result {
				cacheItem := getCacheItem(hashKey, sha256)
				if cacheItem.ExpirationTime.Before(time.Now()) {
					removeCacheFile(hashKey, sha256, cacheItem.URL, "Expired")
				}
			}
			rwMutex.Unlock()
		}
		myLogger.logger.Printf("Cleanup Expired Items\n")
	}
}

func removeCacheFile(hashKey int, sha256 string, url string, logMsg string) {
	_, err := redisClient.HDel(strconv.Itoa(hashKey), sha256).Result() //_ : 지워진 값 개수
	if err != nil {
		panic(err)
	}
	myLogger.logger.Printf("%s) 캐시가 삭제되었습니다 : %s\n", logMsg, url)
}

func logPerSec() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		myLogger.LogCacheNum()
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

func (mLogger *MyLogger) LogCacheNum() {
	countData.rwMutex.Lock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Cached File Number = %d, Send cache file number = %d", countData.cachedFile, countData.sendCache)
	mLogger.logger.Println(sb.String())

	countData.cachedFile = 0
	countData.sendCache = 0
	countData.rwMutex.Unlock()
}