// Copyright © 2017 Kenny Freeman kenny.freeman@gmail.com
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package db

import (
	"github.com/allegro/bigcache"
	"github.com/s4z/prom2click/config"
)

// NewCache creates a new Cache from the provided configuration
func NewCache(cfg config.Provider) (*bigcache.BigCache, error) {
	c, err := bigcache.NewBigCache(bigcache.Config{
		Shards:             cfg.GetInt("cache.shards"),
		LifeWindow:         cfg.GetDuration("cache.ttl"),
		MaxEntriesInWindow: cfg.GetInt("cache.items"),
		MaxEntrySize:       cfg.GetInt("cache.item_size"),
		Verbose:            cfg.GetBool("cache.verbose"),
		HardMaxCacheSize:   cfg.GetInt("cache.max_size"),
		// possibly todo: Hasher
		// todo: OnRemove func(key string, entry []byte)
		// add metrics on cache evictions
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}
