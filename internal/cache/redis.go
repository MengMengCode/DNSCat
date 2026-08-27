package cache

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"dnscat/internal/config"

	"github.com/redis/go-redis/v9"
)

type CacheService struct {
	rdb         *redis.Client
	useRedis    bool
	memStore    sync.Map
	subscribers map[string][]chan string
	subMu       sync.RWMutex
}

var GlobalCache *CacheService

func InitCache(cfg *config.Config) *CacheService {
	cs := &CacheService{
		subscribers: make(map[string][]chan string),
	}

	if cfg.Redis.Enabled && cfg.Redis.Addr != "" {
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := rdb.Ping(ctx).Err(); err == nil {
			log.Printf("[Cache] Redis connected successfully at %s", cfg.Redis.Addr)
			cs.rdb = rdb
			cs.useRedis = true
		} else {
			log.Printf("[Cache] Redis connection failed (%v), falling back to in-memory cache", err)
		}
	} else {
		log.Printf("[Cache] In-memory cache initialized")
	}

	GlobalCache = cs
	return cs
}

func (c *CacheService) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	if c.useRedis && c.rdb != nil {
		return c.rdb.Set(ctx, key, data, expiration).Err()
	}

	c.memStore.Store(key, string(data))
	return nil
}

func (c *CacheService) Get(ctx context.Context, key string, target interface{}) error {
	if c.useRedis && c.rdb != nil {
		data, err := c.rdb.Get(ctx, key).Bytes()
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}

	val, ok := c.memStore.Load(key)
	if !ok {
		return redis.Nil
	}

	return json.Unmarshal([]byte(val.(string)), target)
}

func (c *CacheService) Delete(ctx context.Context, key string) error {
	if c.useRedis && c.rdb != nil {
		return c.rdb.Del(ctx, key).Err()
	}
	c.memStore.Delete(key)
	return nil
}

// RedisAvailable 表示基于 Redis 的持久化能力是否可用。
// 安全日志用 Redis LIST 做 FIFO 持久化；内存降级模式下不提供该能力，
// 调用方据此决定是否退化成「仅内存、重启即清空」。
func (c *CacheService) RedisAvailable() bool {
	return c.useRedis && c.rdb != nil
}

// ListPushCapped 把 items（按时间升序）压入列表头部，并把列表裁剪到最多 limit 条。
// LPUSH + LTRIM 合并进一个 pipeline 下发，只有一次网络往返；
// 这正是「写满上限后丢弃最旧的一条、始终保留最新的」的 O(1) 实现，
// 无需像 SQL 那样先 count 再 delete。
func (c *CacheService) ListPushCapped(ctx context.Context, key string, items []string, limit int) error {
	if !c.RedisAvailable() || len(items) == 0 {
		return nil
	}
	values := make([]interface{}, 0, len(items))
	for _, it := range items {
		values = append(values, it)
	}
	pipe := c.rdb.Pipeline()
	// LPUSH 传多值时，越靠后的参数越靠近表头；传入升序即得到「最新在头部」的列表。
	pipe.LPush(ctx, key, values...)
	if limit > 0 {
		pipe.LTrim(ctx, key, 0, int64(limit-1))
	}
	_, err := pipe.Exec(ctx)
	return err
}

// ListRange 返回列表指定区间的元素（下标 0 起，-1 表示末尾）。
// 表头是最新写入的元素。
func (c *CacheService) ListRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	if !c.RedisAvailable() {
		return nil, nil
	}
	return c.rdb.LRange(ctx, key, start, stop).Result()
}

// ListTrim 把列表裁剪到最多 limit 条，保留表头最新的部分。
// 用于保留上限被调小后立即收敛已存储的日志量。
func (c *CacheService) ListTrim(ctx context.Context, key string, limit int) error {
	if !c.RedisAvailable() || limit <= 0 {
		return nil
	}
	return c.rdb.LTrim(ctx, key, 0, int64(limit-1)).Err()
}

func (c *CacheService) Publish(ctx context.Context, channel string, message string) error {
	if c.useRedis && c.rdb != nil {
		return c.rdb.Publish(ctx, channel, message).Err()
	}

	c.subMu.RLock()
	defer c.subMu.RUnlock()

	if chs, ok := c.subscribers[channel]; ok {
		for _, ch := range chs {
			select {
			case ch <- message:
			default:
			}
		}
	}
	return nil
}

func (c *CacheService) Subscribe(ctx context.Context, channel string) <-chan string {
	out := make(chan string, 100)

	if c.useRedis && c.rdb != nil {
		go func() {
			pubsub := c.rdb.Subscribe(ctx, channel)
			defer pubsub.Close()

			ch := pubsub.Channel()
			for {
				select {
				case <-ctx.Done():
					close(out)
					return
				case msg, ok := <-ch:
					if !ok {
						close(out)
						return
					}
					out <- msg.Payload
				}
			}
		}()
		return out
	}

	c.subMu.Lock()
	c.subscribers[channel] = append(c.subscribers[channel], out)
	c.subMu.Unlock()

	go func() {
		<-ctx.Done()
		c.subMu.Lock()
		defer c.subMu.Unlock()
		chs := c.subscribers[channel]
		for i, ch := range chs {
			if ch == out {
				c.subscribers[channel] = append(chs[:i], chs[i+1:]...)
				close(out)
				break
			}
		}
	}()

	return out
}
