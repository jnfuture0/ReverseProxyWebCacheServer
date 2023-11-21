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
	GZIP             string = "gzip"
	GLOBAL_HOST      string = "global.gmarket.co.kr"
	IMAGE_HOST       string = "image.gmarket.co.kr"
	CUSTOM_HOST      string = "jn.wcs.co.kr"
	CACHED           string = " (Cached)"
	NOT_CACHED       string = " (Not cached)"
	CONFIG_PATH      string = "./wcs/config.json"
	WCS_PATH         string = "./wcs/"
	LOCK             string = "LOCK"
	RLOCK            string = "RLOCK"
	STORE_TYPE_REDIS string = "redis"
	STORE_TYPE_FILE  string = "file"
)

var (
	rwMutextList      []*sync.RWMutex
	redisClient       *redis.Client //key = hashKey, field = sha256
	safeCacheItemList []*safeCacheItem
	config            = Config{}
	myLogger          *MyLogger
	countData         = countDatas{&sync.RWMutex{}, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
)

type safeCacheItem struct {
	cacheItemMap map[string]CacheItem
}

type countDatas struct {
	rwMutex           *sync.RWMutex
	sendCache         int
	cachedFile        int
	gHit              int
	gRequest          int
	iHit              int
	iRequest          int
	filesizeError     int
	cacheException    int
	statusError       int
	methodError       int
	cacheControlError int
	contentTypeError  int
}

type CacheItem struct {
	Header         http.Header
	Body           []byte
	URL            string
	Host           string
	Dir            string
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
	StoreType             string   `json:"StoreType"`
}

type proxyHandler struct {
	proxy map[string]*httputil.ReverseProxy
}

type HTMLData struct {
	HitData          []htmlHitData
	ConfigData       []htmlConfigData
	CacheData        htmlCacheData
	ReasonsNotCached htmlReasonsNotCached
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
type htmlReasonsNotCached struct {
	FileSizeError     int
	CacheException    int
	StatusError       int
	MethodError       int
	CacheControlError int
	ContentTypeError  int
	Total             int
}

func init() {}

func OpenServer() {
	initMutexAndRedis()
	defer redisClient.Close()

	loadConfig()

	//For test
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
	os.RemoveAll(WCS_PATH + "log_body")
	os.RemoveAll(WCS_PATH + "log_image")
	os.Remove(WCS_PATH + "log_file.txt")
	redisClient.FlushAll().Result()
	fmt.Println("Remove All cache")
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

func initMutexAndRedis() {
	for i := 0; i < 255; i++ {
		rw := &sync.RWMutex{}
		rwMutextList = append(rwMutextList, rw)
		safeCacheItem := &safeCacheItem{make(map[string]CacheItem)}
		safeCacheItemList = append(safeCacheItemList, safeCacheItem)
	}

	redisClient = redis.NewClient(&redis.Options{
		Addr:     "192.168.0.89:6379",
		Password: "",
		DB:       0,
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

	increaseRequestsCount(r.Host)

	uri := GetURI(r)
	sha256 := GetSha256(uri)
	hashKey := GetHashkey(uri)

	startTime := time.Now()
	var isCached string
	if isCacheExist(hashKey, sha256) {
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
		increaseCountData(&countData.cacheException)
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
		increaseCountData(&countData.filesizeError)
		return nil
	}

	go cacheFile(body, resp)

	return nil
}

func increaseRequestsCount(host string) {
	switch host {
	case GLOBAL_HOST:
		increaseCountData(&countData.gRequest)
	case IMAGE_HOST:
		increaseCountData(&countData.iRequest)
	}
}

func increaseHitCount(host string) {
	switch host {
	case GLOBAL_HOST:
		increaseCountData(&countData.gHit, &countData.sendCache)
	case IMAGE_HOST:
		increaseCountData(&countData.iHit, &countData.sendCache)
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

	rnc := htmlReasonsNotCached{
		countData.filesizeError,
		countData.cacheException,
		countData.statusError,
		countData.methodError,
		countData.cacheControlError,
		countData.contentTypeError,
		countData.filesizeError + countData.cacheException + countData.statusError + countData.methodError + countData.cacheControlError + countData.contentTypeError,
	}

	tmpl, err := template.ParseFiles(WCS_PATH + "status-page.html")
	if err != nil {
		panic(err)
	}

	htmlData := HTMLData{htmlDataList, configDataList, getCachedData(), rnc}
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

	matchCount := 0

	removeMatchFile := func(hashKey int, sha256 string, ci CacheItem) {
		if compiledPattern.MatchString(ci.URL) {
			removeCacheFile(hashKey, sha256, ci.URL, "Purge")
			matchCount += 1
		}
	}

	doForEachCachedData(LOCK, removeMatchFile)
	fmt.Fprintf(w, "Purge Success! (%d items)\n", matchCount)
}

func getCachedData() (cachedData htmlCacheData) {
	appendEachData := func(hashKey int, sha256 string, ci CacheItem) {
		switch ci.Host {
		case IMAGE_HOST:
			cachedData.ImageData = append(cachedData.ImageData, ci.URL)
			cachedData.ImageDataCount += 1
		default:
			cachedData.GlobalData = append(cachedData.GlobalData, ci.URL)
			cachedData.GlobalDataCount += 1
		}
	}

	doForEachCachedData(RLOCK, appendEachData)
	sort.Slice(cachedData.ImageData, func(i, j int) bool {
		return cachedData.ImageData[i] < cachedData.ImageData[j]
	})
	sort.Slice(cachedData.GlobalData, func(i, j int) bool {
		return cachedData.GlobalData[i] < cachedData.GlobalData[j]
	})

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

	increaseHitCount(r.Host)
}

func getCacheItem(hashKey int, sha256 string) CacheItem {
	var ci CacheItem

	switch config.StoreType {
	case STORE_TYPE_REDIS:
		ciJSON, err := redisClient.HGet(strconv.Itoa(hashKey), sha256).Result()
		if err != nil {
			panic(err)
		}
		json.Unmarshal([]byte(ciJSON), &ci)
	case STORE_TYPE_FILE:
		ci = safeCacheItemList[hashKey].cacheItemMap[sha256]
	default:
		panic("StoreTypeError")
	}
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

func isCacheExist(hashKey int, sha256 string) bool {
	rwMutextList[hashKey].RLock()
	defer rwMutextList[hashKey].RUnlock()

	switch config.StoreType {
	case STORE_TYPE_REDIS:
		exists, err := redisClient.HExists(strconv.Itoa(hashKey), sha256).Result()
		if err != nil {
			panic(err)
		}
		return exists
	case STORE_TYPE_FILE:
		safeCacheItem := safeCacheItemList[hashKey]
		cacheItem, exists := safeCacheItem.cacheItemMap[sha256]

		_, err := os.Stat(cacheItem.Dir)
		return !os.IsNotExist(err) && exists
	default:
		panic("StoreTypeError")
	}
}

// StatueCode, Method, Cache-Control, Content-Type 확인
func checkHeaderCacheSave(resp *http.Response) bool {
	url := resp.Request.URL

	//Check Status Code
	if resp.StatusCode != http.StatusOK {
		myLogger.logger.Printf("CheckHeader : Status not ok. StatusCode = %d, %s\n", resp.StatusCode, url)
		increaseCountData(&countData.statusError)
		return false
	}

	//Check Method
	if resp.Request.Method != http.MethodGet && resp.Request.Method != http.MethodHead {
		increaseCountData(&countData.methodError)
		return false
	}

	//Check Cache Control
	cacheControl := resp.Header.Get("Cache-Control")
	if !IsCacheControlSaveAllowed(cacheControl) {
		myLogger.logger.Printf("CheckHeader : Cache-Control Not Allowed (%s) : %s\n", cacheControl, url)
		increaseCountData(&countData.cacheControlError)
		return false
	}

	//Check Content Type
	contentType := resp.Header.Get("Content-Type")
	if !IsContentTypeSaveAllowed(contentType) {
		myLogger.logger.Printf("CheckHeader : Cache save not allowd by Content-Type (%s) : %s\n", contentType, url)
		increaseCountData(&countData.contentTypeError)
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
	var dirName string
	if resp.Request.URL.Host == IMAGE_HOST {
		dirName = WCS_PATH + "log_image"
	} else {
		dirName = WCS_PATH + "log_body"
	}

	ci := CacheItem{
		Header:         resp.Header,
		Body:           body,
		URL:            resp.Request.URL.String(),
		Host:           resp.Request.Host,
		Dir:            dirName + "/" + sha256,
		ExpirationTime: getExpirationTime(resp.Header.Get("Cache-Control")),
		CachedTime:     time.Now(),
	}

	rwMutextList[hashKey].Lock()
	switch config.StoreType {
	case STORE_TYPE_REDIS:
		ciJSON, _ := json.Marshal(ci)
		err := redisClient.HSet(strconv.Itoa(hashKey), sha256, ciJSON).Err()
		if err != nil {
			panic(err)
		}
	case STORE_TYPE_FILE:
		os.MkdirAll(dirName, os.ModePerm)
		os.WriteFile(dirName+"/"+sha256, body, 0644)
		safeCacheItemList[hashKey].cacheItemMap[sha256] = ci
	default:
		panic("StoreTypeError")
	}
	rwMutextList[hashKey].Unlock()

	countData.rwMutex.Lock()
	countData.cachedFile += 1
	countData.rwMutex.Unlock()
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

	removeExpired := func(hashKey int, sha256 string, ci CacheItem) {
		if ci.ExpirationTime.Before(time.Now()) {
			removeCacheFile(hashKey, sha256, ci.URL, "Expired")
		}
	}

	for range ticker.C {
		doForEachCachedData(LOCK, removeExpired)
		myLogger.logger.Printf("Cleanup Expired Items\n")
	}
}

func removeCacheFile(hashKey int, sha256 string, url string, logMsg string) {
	switch config.StoreType {
	case STORE_TYPE_REDIS:
		_, err := redisClient.HDel(strconv.Itoa(hashKey), sha256).Result() //_ : 지워진 값 개수
		if err != nil {
			panic(err)
		}
	case STORE_TYPE_FILE:
		ciMap := safeCacheItemList[hashKey].cacheItemMap
		err := os.Remove(ciMap[sha256].Dir)
		if err != nil {
			panic(err)
		}
		delete(ciMap, sha256)
	default:
		panic("StoreTypeError")
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

func increaseCountData(targets ...*int) {
	countData.rwMutex.Lock()
	defer countData.rwMutex.Unlock()
	for _, target := range targets {
		*target += 1
	}
}

func doForEachCachedData(lock string, do func(hashKey int, sha256 string, ci CacheItem)) {
	doRedis := func(lock string, do func(hashKey int, sha256 string, ci CacheItem)) {
		for hashKey, rwMutex := range rwMutextList {
			switch lock {
			case LOCK:
				rwMutex.Lock()
			case RLOCK:
				rwMutex.RLock()
			}

			result, err := redisClient.HGetAll(strconv.Itoa(hashKey)).Result()
			if err != nil {
				panic(err)
			}

			for sha256, _ := range result {
				ci := getCacheItem(hashKey, sha256)
				do(hashKey, sha256, ci)
			}

			switch lock {
			case LOCK:
				rwMutex.Unlock()
			case RLOCK:
				rwMutex.RUnlock()
			}
		}
	}

	doFile := func(lock string, do func(hashKey int, sha256 string, ci CacheItem)) {
		for hashKey, sci := range safeCacheItemList {
			switch lock {
			case LOCK:
				rwMutextList[hashKey].Lock()
			case RLOCK:
				rwMutextList[hashKey].RLock()
			}

			for sha256, ci := range sci.cacheItemMap {
				do(hashKey, sha256, ci)
			}

			switch lock {
			case LOCK:
				rwMutextList[hashKey].Unlock()
			case RLOCK:
				rwMutextList[hashKey].RUnlock()
			}
		}
	}

	switch config.StoreType {
	case STORE_TYPE_REDIS:
		doRedis(LOCK, do)
	case STORE_TYPE_FILE:
		doFile(LOCK, do)
	default:
		panic("StoreTypeError")
	}
}
