/**
  Copyright (c) 2022 Arpabet, LLC. All rights reserved.
*/

package pebblestorage

import (
	"github.com/cockroachdb/pebble"
	"github.com/pkg/errors"
)

var (

	WriteOptions = pebble.NoSync

	ErrInvalidFormat    = errors.New("invalid format")
	ErrCanceled         = errors.New("operation was canceled")
)

