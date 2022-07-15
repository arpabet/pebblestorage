/**
  Copyright (c) 2022 Arpabet, LLC. All rights reserved.
*/

package pebblestorage

import (
	"github.com/cockroachdb/pebble"
)

func OpenDatabase(dataDir string, opts *pebble.Options) (*pebble.DB, error) {
	return pebble.Open(dataDir, opts)
}

