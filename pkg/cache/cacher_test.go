// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cache

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

type testStruct struct {
	Public string
	In     testEmbeddedStruct
}

type testEmbeddedStruct struct {
	Public []byte
}

func testRandomKey(tb testing.TB) string {
	tb.Helper()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		tb.Fatal(err)
	}
	return base64.RawStdEncoding.EncodeToString(b)
}

func exerciseCacher(t *testing.T, cacher Cacher) {
	cases := []struct {
		name string
		do   func(tb testing.TB)
	}{
		{
			name: "int",
			do: func(tb testing.TB) {
				in := 5
				var out int
				exerciseType(tb, cacher, in, &out)
			},
		},
		{
			name: "int64",
			do: func(tb testing.TB) {
				in := int64(5)
				var out int64
				exerciseType(tb, cacher, in, &out)
			},
		},
		{
			name: "float",
			do: func(tb testing.TB) {
				in := 5.0
				var out float64
				exerciseType(tb, cacher, in, &out)
			},
		},
		{
			name: "string",
			do: func(tb testing.TB) {
				in := int64(5)
				var out int64
				exerciseType(tb, cacher, in, &out)
			},
		},
		{
			name: "struct",
			do: func(tb testing.TB) {
				in := testStruct{
					Public: "foo",
					In: testEmbeddedStruct{
						Public: []byte("\x12"),
					},
				}
				var out testStruct
				exerciseType(tb, cacher, in, &out)
			},
		},
	}

	t.Run("exercise", func(t *testing.T) {
		for _, tc := range cases {
			tc := tc

			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				tc.do(t)
			})
		}
	})

	t.Run("close", func(t *testing.T) {
		ctx := context.Background()

		if err := cacher.Close(); err != nil {
			t.Fatal(err)
		}

		key := &Key{
			Namespace: "foo",
			Key:       "bar",
		}

		if err := cacher.Read(ctx, key, nil); !errors.Is(err, ErrStopped) {
			t.Errorf("expected %#v to be %#v", err, ErrStopped)
		}
		if err := cacher.Write(ctx, key, nil, 0); !errors.Is(err, ErrStopped) {
			t.Errorf("expected %#v to be %#v", err, ErrStopped)
		}
		if err := cacher.Delete(ctx, key); !errors.Is(err, ErrStopped) {
			t.Errorf("expected %#v to be %#v", err, ErrStopped)
		}
		if err := cacher.Fetch(ctx, key, nil, 0, nil); !errors.Is(err, ErrStopped) {
			t.Errorf("expected %#v to be %#v", err, ErrStopped)
		}

		// Closing again should not panic
		if err := cacher.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func exerciseType(tb testing.TB, cacher Cacher, in, out interface{}) {
	tb.Helper()

	ctx := context.Background()
	key := &Key{
		Namespace: testRandomKey(tb),
		Key:       testRandomKey(tb),
	}

	// Ensure the value isn't cached already
	if err := cacher.Read(ctx, key, out); !errors.Is(err, ErrNotFound) {
		tb.Fatalf("expected %#v to be %#v", err, ErrNotFound)
	}

	// Write value that's expired
	if err := cacher.Write(ctx, key, in, 10*time.Millisecond); err != nil {
		tb.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)

	// Ensure value is expired
	if err := cacher.Read(ctx, key, out); !errors.Is(err, ErrNotFound) {
		tb.Fatalf("expected %#v to be %#v", err, ErrNotFound)
	}

	// Write value missing func
	if err := cacher.Fetch(ctx, key, out, 1*time.Second, nil); !errors.Is(err, ErrMissingFetchFunc) {
		tb.Fatalf("expected %#v to be %#v", err, ErrMissingFetchFunc)
	}

	// Write value returns error
	if err := cacher.Fetch(ctx, key, out, 1*time.Second, func() (interface{}, error) {
		return nil, fmt.Errorf("nope")
	}); err == nil {
		tb.Fatal("expected error")
	}

	// Write value through cache
	if err := cacher.Fetch(ctx, key, out, 10*time.Minute, func() (interface{}, error) {
		return in, nil
	}); err != nil {
		tb.Fatal(err)
	}

	// Ensure value is cached
	if err := cacher.Read(ctx, key, out); err != nil {
		tb.Fatal(err)
	}
	if got, want := in, reflect.ValueOf(out).Elem().Interface(); !reflect.DeepEqual(got, want) {
		tb.Fatalf("expected %#v to be %#v", got, want)
	}

	// Delete cached value
	if err := cacher.Delete(ctx, key); err != nil {
		tb.Fatal(err)
	}

	// Ensure value is deleted
	if err := cacher.Read(ctx, key, out); !errors.Is(err, ErrNotFound) {
		tb.Fatalf("expected %#v to be %#v", err, ErrNotFound)
	}

	// Create caches with the same namespace
	for i := 0; i < 10; i++ {
		key = &Key{
			Namespace: key.Namespace,
			Key:       testRandomKey(tb),
		}
		if err := cacher.Write(ctx, key, in, 5*time.Minute); err != nil {
			tb.Fatal(err)
		}
	}

	// Delete the prefix
	if err := cacher.DeletePrefix(ctx, key.Namespace); err != nil {
		tb.Fatal(err)
	}

	// Ensure value is deleted
	if err := cacher.Read(ctx, key, out); !errors.Is(err, ErrNotFound) {
		tb.Fatalf("expected %#v to be %#v", err, ErrNotFound)
	}
}
