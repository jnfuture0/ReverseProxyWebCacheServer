package cache

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis"
)

type Cache interface {
	Init()
	Close()
	Clear() //For Test
	Get(hashKey int, sha256 string) (ci CacheItem, exist bool)
	GetAll() (ciList []CacheData)
	Set(hashKey int, sha256 string, ci CacheItem)
	Del(hashKey int, sha256 string)
}

type RedisCache struct {
	RedisClient *redis.Client
}

type FileCache struct {
	SciList []*SafeCacheItem
}

type SafeCacheItem struct {
	RW    *sync.RWMutex
	CiMap map[string]CacheItem
}

type CacheItem struct {
	Header         http.Header
	Body           []byte
	URL            string
	Host           string
	Filepath       string
	ExpirationTime time.Time
	CachedTime     time.Time
}

type CacheData struct {
	HashKey int
	Sha256  string
	Ci      CacheItem
}

func (fc *FileCache) Clear() { //For Test
	wcsPath := "./wcs/"
	os.RemoveAll(wcsPath + "log_body")
	os.RemoveAll(wcsPath + "log_image")
	os.Remove(wcsPath + "log_file.txt")
}

func (fc *FileCache) Close() {}

func (fc *FileCache) Init() {
	for i := 0; i < 255; i++ {
		sci := &SafeCacheItem{
			RW:    &sync.RWMutex{},
			CiMap: make(map[string]CacheItem),
		}
		fc.SciList = append(fc.SciList, sci)
	}
}

func (fc *FileCache) Get(hashKey int, sha256 string) (ci CacheItem, exist bool) {
	sci := fc.SciList[hashKey]
	sci.RW.RLock()
	defer sci.RW.RUnlock()

	ci, exist = sci.CiMap[sha256]

	if exist {
		filebody, err := os.ReadFile(ci.Filepath)
		if err != nil {
			panic(err)
		}
		ci.Body = filebody
	}

	return ci, exist
}

func (fc *FileCache) GetAll() (cacheDataList []CacheData) {
	for hashKey, sci := range fc.SciList {
		sci.RW.RLock()
		for sha256, ci := range sci.CiMap {
			cd := CacheData{hashKey, sha256, ci}
			cacheDataList = append(cacheDataList, cd)
		}
		sci.RW.RUnlock()
	}
	return cacheDataList
}

func (fc *FileCache) Set(hashKey int, sha256 string, ci CacheItem) {
	sci := fc.SciList[hashKey]
	sci.RW.Lock()
	defer sci.RW.Unlock()

	filePath := ci.Filepath
	os.MkdirAll(filepath.Dir(filePath), os.ModePerm)
	os.WriteFile(filePath, ci.Body, 0644)
	sci.CiMap[sha256] = ci
}

func (fc *FileCache) Del(hashKey int, sha256 string) {
	sci := fc.SciList[hashKey]
	sci.RW.Lock()
	defer sci.RW.Unlock()

	_, exist := sci.CiMap[sha256]
	if exist {
		err := os.Remove(sci.CiMap[sha256].Filepath)
		if err != nil {
			panic(err)
		}
		delete(sci.CiMap, sha256)
	}
}

//
//
// Redis

func (rc *RedisCache) Clear() { //For Test
	rc.RedisClient.FlushAll().Result()
	wcsPath := "./wcs/"
	os.RemoveAll(wcsPath + "log_body")
	os.RemoveAll(wcsPath + "log_image")
	os.Remove(wcsPath + "log_file.txt")
}

func (rc *RedisCache) Init() {
	rc.RedisClient = redis.NewClient(&redis.Options{
		Addr:     "192.168.0.89:6379",
		Password: "",
		DB:       0,
	})
}

func (rc *RedisCache) Close() {
	rc.RedisClient.Close()
}

func (rc *RedisCache) Get(hashKey int, sha256 string) (ci CacheItem, exist bool) {
	exist, err := rc.RedisClient.HExists(strconv.Itoa(hashKey), sha256).Result()
	if err != nil {
		panic(err)
	}
	if exist {
		ciJSON, err := rc.RedisClient.HGet(strconv.Itoa(hashKey), sha256).Result()
		if err != nil {
			panic(err)
		}
		json.Unmarshal([]byte(ciJSON), &ci)
	}

	return ci, exist
}

func (rc *RedisCache) GetAll() (ciList []CacheData) {
	for hashKey := 0; hashKey < 255; hashKey++ {
		result, err := rc.RedisClient.HGetAll(strconv.Itoa(hashKey)).Result()
		if err != nil {
			panic(err)
		}

		for sha256, ciJSON := range result {
			ci := CacheItem{}
			json.Unmarshal([]byte(ciJSON), &ci)
			cd := CacheData{hashKey, sha256, ci}
			ciList = append(ciList, cd)
		}
	}
	return ciList
}

func (rc *RedisCache) Set(hashKey int, sha256 string, ci CacheItem) {
	ciJSON, _ := json.Marshal(ci)
	err := rc.RedisClient.HSet(strconv.Itoa(hashKey), sha256, ciJSON).Err()
	if err != nil {
		panic(err)
	}
}

func (rc *RedisCache) Del(hashKey int, sha256 string) {
	_, err := rc.RedisClient.HDel(strconv.Itoa(hashKey), sha256).Result() //_ : 지워진 값 개수
	if err != nil {
		panic(err)
	}
}
