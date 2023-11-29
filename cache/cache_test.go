package cache_test

import (
	"jnlee/cache"
	"strconv"
	"sync"
	"testing"
)

const (
	TEST_DIR = "./test_dir"
)

var (
	fileCacheMock  = cache.FileCache{}
	redisCacheMock = cache.RedisCache{}
)

func init() {
	fileCacheMock.Init()
	redisCacheMock.Init()
}

func TestRaceCondition(t *testing.T) {
	var wg sync.WaitGroup

	wg.Add(4)
	go func() {
		setDatas()
		wg.Done()
	}()
	go func() {
		getDatas()
		wg.Done()
	}()
	go func() {
		getAll()
		wg.Done()
	}()
	go func() {
		delDatas()
		wg.Done()
	}()

	wg.Wait()
}

func setDatas() {
	for i := 0; i < 255; i++ {
		ci := cache.CacheItem{
			Body:     []byte("str"),
			URL:      "dummy_url_" + strconv.Itoa(i),
			Host:     "dummy_host_" + strconv.Itoa(i),
			Filepath: TEST_DIR + "/test_" + "_" + strconv.Itoa(i),
		}
		// fileCacheMock.Set(i, "sha_"+strconv.Itoa(i), ci)
		redisCacheMock.Set(i, "sha"+"_"+strconv.Itoa(i), ci)
	}
}

func getDatas() {
	for i := 0; i < 255; i++ {
		// fileCacheMock.Get(i, "sha_"+strconv.Itoa(i))
		redisCacheMock.Get(i, "sha_"+strconv.Itoa(i))
	}
}

func getAll() {
	// for i := 0; i < 100; i++ {
	// fileCacheMock.GetAll()
	redisCacheMock.GetAll()
	// }
}

func delDatas() {
	for i := 0; i < 255; i++ {
		// fileCacheMock.Del(i, "sha"+strconv.Itoa(i))
		redisCacheMock.Del(i, "sha"+strconv.Itoa(i))
	}
}
