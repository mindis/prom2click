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
	"database/sql"
	"fmt"

	// blank import needed for clickhouse sql driver
	_ "github.com/kshvakov/clickhouse"
	"github.com/s4z/prom2click/config"
)

// NewConn creates a new DB from the provided configuration
func NewDB(cfg config.Provider) (*sql.DB, error) {
	var (
		db  *sql.DB
		err error
	)
	db, err = sql.Open("clickhouse", cfg.GetString("clickhouse.dsn"))
	if err != nil {
		fmt.Printf("Error connecting to clickhouse: %s\n", err.Error())
		return nil, err
	}

	return db, nil
}
