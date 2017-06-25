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
package config

import (
	"time"

	"fmt"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

// LoadConfig loads a prom2click configuration into a new Viper and
// adds defaults. Adapted from hugo static site gen.
func LoadConfig(fs afero.Fs, relativePath, cfgFilename string) (*viper.Viper, error) {
	fmt.Println("LoadConfig")
	v := viper.New()
	v.SetFs(fs)

	// set cwd if no path provided
	if relativePath == "" {
		relativePath = "."
	}

	v.AutomaticEnv()
	v.SetEnvPrefix("p2c")
	v.SetConfigFile(cfgFilename)
	v.AddConfigPath(relativePath)

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	loadDefaults(v)
	return v, nil
}

func loadDefaults(v *viper.Viper) {
	fmt.Println("loadDefaults")
	v.SetDefault("clickhouse.dsn",
		"tcp://127.0.0.1:9000?username=&password=&database=metrics&"+
			"read_timeout=10&write_timeout=10&alt_hosts=",
	)
	v.SetDefault("clickhouse.db", "metrics")
	v.SetDefault("clickhouse.tbl_samples", "samples")
	v.SetDefault("clickhouse.tbl_metrics", "metrics")
	v.SetDefault("clickhouse.tbl_collisions", "collisions")
	v.SetDefault("clickhouse.batch", 100)
	v.SetDefault("clickhouse.fetch", 8192)
	v.SetDefault("clickhouse.quantile", 0.75)
	v.SetDefault("clickhouse.minPeriod", 10)

	// number of shards for cache, must be power of 2
	v.SetDefault("cache.shards", 8192)
	// try to keep entries in cache for this long
	v.SetDefault("cache.ttl", time.Hour*48)
	// initial size hint on cache creation
	v.SetDefault("cache.items", 10000)
	v.SetDefault("cache.item_size", 16)
	// print details of cache memory allocations
	v.SetDefault("cache.verbose", false)
	// max size of cache in MB
	v.SetDefault("cache.max_size", 1024)

	v.SetDefault("buffer", 8192)
	v.SetDefault("http.listen", ":9201")
	v.SetDefault("http.write", "/write")
	v.SetDefault("http.read", "/read")
	v.SetDefault("http.metrics", "/metrics")
	v.SetDefault("http.timeout", 30*time.Second)
}
