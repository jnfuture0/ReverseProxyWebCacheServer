package wcs

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"jnlee/workerpool"
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
	LOCK_STRING      string = "LOCK"
	RLOCK_STRING     string = "RLOCK"
	STORE_TYPE_REDIS string = "redis"
	STORE_TYPE_FILE  string = "file"
)

var (
	//key = hashKey, field = sha256
	redisClient       *redis.Client
	safeCacheItemList []*safeCacheItem
	Config            = ConfigStruct{}
	myLogger          *MyLogger
	countData         countDatasForStatusPage
	Workerpool        workerpool.WorkerPool
)

type safeCacheItem struct {
	rwMutex *sync.RWMutex
	ciMap   map[string]CacheItem
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

type countDatasForStatusPage struct {
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

type MyLogger struct {
	logger *log.Logger
}

type ConfigStruct struct {
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
	ShowImage       bool
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
	loadConfig()

	InitWorkerpool()

	InitCountDatas()

	switch Config.StoreType {
	case STORE_TYPE_REDIS:
		InitRedis()
		defer redisClient.Close()
	case STORE_TYPE_FILE:
		InitMutexList()
	}

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
	if Config.StoreType == "redis" {
		redisClient.FlushAll().Result()
	}
	fmt.Println("Remove All cache")
}

func loadConfig() {
	configData, err := os.ReadFile(CONFIG_PATH)
	if err != nil {
		panic(err)
	}

	newConfig := &ConfigStruct{}
	err = json.Unmarshal(configData, newConfig)
	if err != nil {
		panic(err)
	}
	Config = *newConfig
}

func InitMutexList() {
	for i := 0; i < 255; i++ {
		rw := &sync.RWMutex{}
		ciMap := &safeCacheItem{rw, make(map[string]CacheItem)}
		safeCacheItemList = append(safeCacheItemList, ciMap)
	}
}

func InitRedis() {
	redisClient = redis.NewClient(&redis.Options{
		Addr:     "192.168.0.89:6379",
		Password: "",
		DB:       0,
	})
}

func InitWorkerpool() {
	Workerpool = workerpool.NewWorkerPool(255)
	Workerpool.Run()
}

func InitCountDatas() {
	countData = countDatasForStatusPage{&sync.RWMutex{}, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
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

	if r.Host == CUSTOM_HOST {
		switch r.URL.Path {
		case "/statuspage":
			showStatusPage(w, false)
		case "/statuspage-with-image":
			showStatusPage(w, true)
		}
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

	if Config.ResTimeLoggingEnabled {
		elapsedTime := time.Since(startTime)
		myLogger.LogElapsedTime(r.Host+r.URL.Path+isCached, elapsedTime)
	}
}

func modifyResponse(resp *http.Response) error {
	url := resp.Request.URL

	if !isCacheable(resp) {
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
	if len(body) > int(Config.MaxFileSize) {
		myLogger.logger.Printf("File size over. Do not cache : %s (%d bytes)\n", url.String(), len(body))
		increaseCountData(&countData.filesizeError)
		return nil
	}

	Workerpool.AddTask(func() { CacheFile(body, resp) })

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

func showStatusPage(w http.ResponseWriter, showImage bool) {
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

	htmlData := HTMLData{htmlDataList, configDataList, getCachedData(showImage), rnc}
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

	doForEachCachedData(LOCK_STRING, removeMatchFile)
	fmt.Fprintf(w, "Purge Success! (%d items)\n", matchCount)
}

func getCachedData(showImage bool) (cachedData htmlCacheData) {
	cachedData.ShowImage = showImage

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

	doForEachCachedData(RLOCK_STRING, appendEachData)
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
	cacheItem := getCacheItem(hashKey, sha256)
	var fileBody []byte

	switch Config.StoreType {
	case STORE_TYPE_REDIS:
		fileBody = cacheItem.Body
	case STORE_TYPE_FILE:
		var err error
		fileBody, err = os.ReadFile(cacheItem.Dir)
		if err != nil {
			panic(err)
		}
	}

	if Config.GzipEnabled && getIsGzipAccepted(r) {
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

	switch Config.StoreType {
	case STORE_TYPE_REDIS:
		ciJSON, err := redisClient.HGet(strconv.Itoa(hashKey), sha256).Result()
		if err != nil {
			panic(err)
		}
		json.Unmarshal([]byte(ciJSON), &ci)
	case STORE_TYPE_FILE:
		safeCacheItem := safeCacheItemList[hashKey]
		safeCacheItem.rwMutex.RLock()
		ci = safeCacheItem.ciMap[sha256]
		safeCacheItem.rwMutex.RUnlock()
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
	case len(myUrl.Query()) == 0 || Config.QueryIgnoreEnabled:
		return req.Method + host + myUrl.Path
	case Config.QuerySortingEnabled:
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

func IsCacheException(url string) bool {
	regexps := make([]*regexp.Regexp, len(Config.CacheExceptions))
	for i, pattern := range Config.CacheExceptions {
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
	switch Config.StoreType {
	case STORE_TYPE_REDIS:
		exists, err := redisClient.HExists(strconv.Itoa(hashKey), sha256).Result()
		if err != nil {
			panic(err)
		}
		return exists
	case STORE_TYPE_FILE:
		safeCacheItem := safeCacheItemList[hashKey]
		safeCacheItem.rwMutex.RLock()
		cacheItem, exists := safeCacheItem.ciMap[sha256]
		safeCacheItem.rwMutex.RUnlock()

		_, err := os.Stat(cacheItem.Dir)
		return !os.IsNotExist(err) && exists
	default:
		panic("StoreTypeError")
	}
}

// StatueCode, Method, Cache-Control, Content-Type 확인
func isCacheable(resp *http.Response) bool {
	url := resp.Request.URL
	uri := GetURI(resp.Request)

	if IsCacheException(uri) {
		increaseCountData(&countData.cacheException)
		myLogger.logger.Printf("CheckCacheable : CacheException. uri = %s\n", uri)
		return false
	}

	//Check Status Code
	if resp.StatusCode != http.StatusOK {
		myLogger.logger.Printf("CheckCacheable : Status not ok. StatusCode = %d, %s\n", resp.StatusCode, url)
		increaseCountData(&countData.statusError)
		return false
	}

	//Check Method
	if resp.Request.Method != http.MethodGet && resp.Request.Method != http.MethodHead {
		increaseCountData(&countData.methodError)
		myLogger.logger.Printf("CheckCacheable : Method not ok. method = %s\n", resp.Request.Method)
		return false
	}

	//Check Cache Control
	cacheControl := resp.Header.Get("Cache-Control")
	if !IsCacheControlSaveAllowed(cacheControl) {
		myLogger.logger.Printf("CheckCacheable : Cache-Control Not Allowed (%s) : %s\n", cacheControl, url)
		increaseCountData(&countData.cacheControlError)
		return false
	}

	//Check Content Type
	contentType := resp.Header.Get("Content-Type")
	if !IsContentTypeSaveAllowed(contentType) {
		myLogger.logger.Printf("CheckCacheable : Cache save not allowd by Content-Type (%s) : %s\n", contentType, url)
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

func CacheFile(body []byte, resp *http.Response) {
	uri := GetURI(resp.Request)
	sha256 := GetSha256(uri)
	hashKey := GetHashkey(uri)
	ci := CacheItem{
		Header:         resp.Header,
		URL:            resp.Request.URL.String(),
		Host:           resp.Request.Host,
		ExpirationTime: GetExpirationTime(resp.Header.Get("Cache-Control")),
		CachedTime:     time.Now(),
	}

	switch Config.StoreType {
	case STORE_TYPE_REDIS:
		ci.Body = body

		ciJSON, _ := json.Marshal(ci)
		err := redisClient.HSet(strconv.Itoa(hashKey), sha256, ciJSON).Err()
		if err != nil {
			panic(err)
		}
	case STORE_TYPE_FILE:
		var dirName string
		if resp.Request.URL.Host == IMAGE_HOST {
			dirName = WCS_PATH + "log_image"
		} else {
			dirName = WCS_PATH + "log_body"
		}
		ci.Dir = dirName + "/" + sha256

		safeCacheItem := safeCacheItemList[hashKey]
		safeCacheItem.rwMutex.Lock()
		os.MkdirAll(dirName, os.ModePerm)
		os.WriteFile(dirName+"/"+sha256, body, 0644)
		safeCacheItem.ciMap[sha256] = ci
		safeCacheItem.rwMutex.Unlock()
	default:
		panic("StoreTypeError")
	}

	increaseCountData(&countData.cachedFile)
}

func GetExpirationTime(cacheControl string) time.Time {
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
	ticker := time.NewTicker(time.Second * time.Duration(Config.CleanupFrequency))
	defer ticker.Stop()

	removeExpired := func(hashKey int, sha256 string, ci CacheItem) {
		if ci.ExpirationTime.Before(time.Now()) {
			removeCacheFile(hashKey, sha256, ci.URL, "Expired")
		}
	}

	for range ticker.C {
		//Run 'removeExpired' function for every cache data
		doForEachCachedData(LOCK_STRING, removeExpired)
		myLogger.logger.Printf("Cleanup Expired Items\n")
	}
}

func removeCacheFile(hashKey int, sha256 string, url string, logMsg string) {
	switch Config.StoreType {
	case STORE_TYPE_REDIS:
		_, err := redisClient.HDel(strconv.Itoa(hashKey), sha256).Result() //_ : 지워진 값 개수
		if err != nil {
			panic(err)
		}
	case STORE_TYPE_FILE:
		ciMap := safeCacheItemList[hashKey].ciMap
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
	doRedis := func(do func(hashKey int, sha256 string, ci CacheItem)) {
		for hashKey := 0; hashKey < 255; hashKey++ {
			result, err := redisClient.HGetAll(strconv.Itoa(hashKey)).Result()
			if err != nil {
				panic(err)
			}

			for sha256 := range result {
				cacheItem := getCacheItem(hashKey, sha256)
				do(hashKey, sha256, cacheItem)
			}
		}
	}

	doFile := func(lock string, do func(hashKey int, sha256 string, ci CacheItem)) {
		for hashKey, sci := range safeCacheItemList {
			rwMutex := sci.rwMutex
			switch lock {
			case LOCK_STRING:
				rwMutex.Lock()
			case RLOCK_STRING:
				rwMutex.RLock()
			}

			for sha256, ci := range sci.ciMap {
				do(hashKey, sha256, ci)
			}

			switch lock {
			case LOCK_STRING:
				rwMutex.Unlock()
			case RLOCK_STRING:
				rwMutex.RUnlock()
			}
		}
	}

	switch Config.StoreType {
	case STORE_TYPE_REDIS:
		doRedis(do)
	case STORE_TYPE_FILE:
		doFile(lock, do)
	default:
		panic("StoreTypeError")
	}
}
