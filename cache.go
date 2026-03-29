// x := 5 -> var x = 5
//its basically used for new variables 
package main

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type CacheNode struct{
	key string
	response []byte
	expiry time.Time
	lastAccess time.Time
	prev *CacheNode
	next *CacheNode

}

type Cache struct{
	store map[string]*CacheNode
	head *CacheNode //front dummy node (mosty recently used) (left side)
	tail *CacheNode //back dummy node (leasst recently used) (right side)
	//makes this thread-safe, which means multiple goroutines can read from it but only one can write to it at a time
	//there are different types of Mutex, this one allows for many reads and one write at a time 
	mu sync.Mutex
	maxSize int
	hits uint64
	misses uint64
}

//returns the address of a newly created cache 
func NewCache(maxSize int) *Cache{

	//this gives use the address of a sturct with all default values to its variables 
	head := &CacheNode{}
	tail := &CacheNode{}
	head.next = tail
	tail.prev = head

	return &Cache{
		store: make(map[string]*CacheNode),
		head: head,
		tail: tail,
		maxSize: maxSize,
	}
}

func (c *Cache) removeNode(node *CacheNode){

	//removes the node from its current position in the linked list
	node.prev.next = node.next
	node.next.prev = node.prev

}

func (c *Cache) addNodeToFront(node *CacheNode){

	node.prev = c.head
	node.next = c.head.next
	c.head.next.prev = node
	c.head.next = node

}


//having ther parameter before the name means calling the function on that object 
func (c *Cache) Get(key string) ([]byte, bool){

	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()
	
	//entry is a pointer to the cache entry 
	node, ok := c.store[key]

	//cache miss bc key doens't exist 
	if !ok{
		atomic.AddUint64(&c.misses,1)
		return nil,false
	}

	if now.After(node.expiry){
		//deletes the key from map c
		c.removeNode(node)
		delete(c.store, key)
		//ensures no race coinditions since multiple goroutines could do this
		atomic.AddUint64(&c.misses,1)
		return nil,false
	}

	node.lastAccess =now
	atomic.AddUint64(&c.hits,1)
	c.removeNode(node)
	c.addNodeToFront(node)

	return node.response, true 
}

func (c *Cache) Put(key string, response []byte, ttl time.Duration){

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()


	if node, ok := c.store[key]; ok{
		node.response= response
		node.expiry = now.Add(ttl)
		node.lastAccess = now
		c.removeNode(node)

		c.addNodeToFront(node)
		return

	}


	if len(c.store) >= c.maxSize{
		c.evict()
	}

	node := &CacheNode{
		key:        key,
		response:   response,
		expiry:     now.Add(ttl),
		lastAccess: now,
	}
	c.addNodeToFront(node)
	c.store[key] = node

}

//when cache is full and needs to get rid of the least used node
func (c *Cache) evict(){

	lru := c.tail.prev

	if lru == c.head{
		return
	}
	c.removeNode(lru)

	delete(c.store, lru.key)
	log.Printf("Cache evicted (LRU): %s", lru.key)

}

func(c *Cache) StartCleanup(interval time.Duration){
	go func(){
		for {
			time.Sleep(interval)
			now := time.Now()
			c.mu.Lock()
			node := c.tail.prev
			for node != c.head{
				prev := node.prev
				if now.After(node.expiry){
					c.removeNode(node)
					delete(c.store, node.key)
					log.Printf("Cache expired: %s", node.key)
					
				}
				node = prev
			}
			c.mu.Unlock()
		}
	}()
}

func (c *Cache) Stats() {
	hits := atomic.LoadUint64(&c.hits)
	misses := atomic.LoadUint64(&c.misses)
	total := hits + misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}
	log.Printf("Cache stats — hits: %d, misses: %d, hit rate: %.1f%%", hits, misses, hitRate)
}