package main

import (
	"log"
	"sync"
	"time"
)
//the reasopn that this rate limiter is so powerful is bc its constantly refilling, but if you use 
// your tokens at a faster rate than it can refill, then you cannot use the site
type TokenBucket struct{
	tokens float64
	maxTokens float64
	refillRate float64
	lastRefill time.Time
	mu sync.Mutex

}

type RateLimiter struct{
	clients map[string]*TokenBucket
	mu sync.Mutex
}  

func createRateLimiter() *RateLimiter{
	r1 := &RateLimiter{
		clients: make(map[string]*TokenBucket),
	}
	r1.startCleanup()
	return r1
}


func newTokenBucket() *TokenBucket{

	return &TokenBucket{
		tokens:     10,        
		maxTokens:  10,        
		refillRate: 1,         
		lastRefill: time.Now(),
	}
}

func (tb *TokenBucket) refill(){
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds() //current time - last refill time in seconds (*(in float64))
	tb.tokens += elapsed * tb.refillRate //the whole point is that tokens are being refilled continuously

	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now
}

func (tb *TokenBucket) Allow() bool{
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	//this is what makes token bucket rate limiters so unique, if you use your tokens 
	//at a faster rate than it can refill, then you cannot use the site
	if tb.tokens >=1{
		tb.tokens -=1
		return true
	}
	log.Printf("Rate limit exceeded for this TokenBucket" )

	return false
}

// getBucket returns existing bucket for IP or creates a new one
func (r1 *RateLimiter) getBucket(ip string) *TokenBucket{
	r1.mu.Lock()
	defer r1.mu.Unlock()

	if bucket, ok := r1.clients[ip]; ok{
		return bucket
	}

	bucket := newTokenBucket()
	r1.clients[ip] = bucket

	return bucket
}

//is this needed???????
func (r1 *RateLimiter) isAllowed(ip string) bool{
	bucket := r1.getBucket(ip)
	return bucket.Allow()
}

func (r1 *RateLimiter) startCleanup(){
	go func(){
		for{
			time.Sleep(5*time.Minute)
			r1.mu.Lock()
			for ip, bucket := range r1.clients{
				bucket.mu.Lock()
				//checks if the bucket has been used in the past 5 minutes 
				inactive := time.Since(bucket.lastRefill) > 5 * time.Minute
				bucket.mu.Unlock()

				if inactive{
					delete(r1.clients, ip)
					log.Printf("Rate limiter cleanup: removed bucket for IP %s", ip)
				}
			}
			r1.mu.Unlock()
		}
	}()
}