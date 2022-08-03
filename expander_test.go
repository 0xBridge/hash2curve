// SPDX-License-Identifier: MIT
//
// Copyright (C) 2021 Daniel Bourdrez. All Rights Reserved.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree or at
// https://spdx.org/licenses/MIT.html

// Package hash2curve provides hash-to-curve compatible input expansion.
package hash2curve

import (
	"bytes"
	"crypto"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/bytemare/hash"
)

func TestExpander_ZeroDST(t *testing.T) {
	msg := []byte("test")
	zeroDST := []byte("")
	length := 32

	defer func() {
		recover()
	}()

	xmd1 := crypto.SHA256
	_ = ExpandXMD(xmd1, msg, zeroDST, length)

	xof1 := hash.SHAKE128
	_ = ExpandXOF(xof1, msg, zeroDST, length)

	t.Fatal("expected panic on zero length DST")
}

func TestExpander_LongDST(t *testing.T) {
	msg := []byte("test")
	longDST := []byte(
		"a255_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	length := 32

	xmd1 := crypto.SHA256
	_ = ExpandXMD(xmd1, msg, longDST, length)

	xof1 := hash.SHAKE128
	_ = ExpandXOF(xof1, msg, longDST, length)
}

func TestExpander_XMDHighLength(t *testing.T) {
	defer func() {
		recover()
	}()

	length := 9000
	_ = ExpandXMD(crypto.SHA256, []byte("input"), []byte("dst"), length)
	t.Fatal("expected panic on extremely high requested output length")
}

type vector struct {
	dstPrime     []byte
	lenInBytes   int
	msg          []byte
	msgPrime     []byte
	uniformBytes []byte
}

type vectorStrings struct {
	DSTPrime     string `json:"DST_prime"`
	LenInBytes   string `json:"len_in_bytes"`
	Msg          string `json:"msg"`
	MsgPrime     string `json:"msg_prime"`
	UniformBytes string `json:"uniform_bytes"`
}

func (vs *vectorStrings) decode() (*vector, error) {
	v := &vector{}
	var err error

	v.dstPrime, err = hex.DecodeString(vs.DSTPrime)
	if err != nil {
		return nil, err
	}

	length, err := strconv.ParseUint(vs.LenInBytes[2:], 16, 32)
	if err != nil {
		return nil, err
	}

	v.lenInBytes = int(length)
	v.msg = []byte(vs.Msg)

	v.msgPrime, err = hex.DecodeString(vs.MsgPrime)
	if err != nil {
		return nil, err
	}

	v.uniformBytes, err = hex.DecodeString(vs.UniformBytes)
	if err != nil {
		return nil, err
	}

	return v, err
}

type set struct {
	DST   string          `json:"DST"`
	Hash  string          `json:"hash"`
	K     int             `json:"k"`
	Name  string          `json:"name"`
	Tests []vectorStrings `json:"tests"`
}

func mapHash(name string) hash.Identifier {
	switch name {
	case "SHA256":
		return hash.SHA256
	case "SHA512":
		return hash.SHA512
	case "SHAKE128":
		return hash.SHAKE128
	case "SHAKE256":
		return hash.SHAKE256
	default:
		return nil
	}
}

func mapXMD(name string) crypto.Hash {
	switch name {
	case "SHA256":
		return crypto.SHA256
	case "SHA512":
		return crypto.SHA512
	default:
		panic(nil)
	}
}

func mapXOF(name string) hash.Extendable {
	switch name {
	case "SHAKE128":
		return hash.SHAKE128
	case "SHAKE256":
		return hash.SHAKE256
	default:
		panic(nil)
	}
}

func isXMD(id string) bool {
	return id == "SHA256" || id == "SHA512"
}

func concatenate(input ...[]byte) []byte {
	length := 0
	for _, b := range input {
		length += len(b)
	}

	buf := make([]byte, 0, length)

	for _, in := range input {
		buf = append(buf, in...)
	}

	return buf
}

func msgPrime(h hash.Identifier, input, dst []byte, length int) []byte {
	lib := i2osp(length, 2)
	dstPrime := dstPrime(dst)

	if h.Extendable() {
		return concatenate(input, lib, dstPrime)
	}

	zPad := make([]byte, h.BlockSize())
	zeroByte := []byte{0}

	return concatenate(zPad, input, lib, zeroByte, dstPrime)
}

func (s *set) dst() []byte {
	if isXMD(s.Hash) {
		h := mapXMD(s.Hash)
		return vetDSTXMD(h.New(), []byte(s.DST))
	} else {
		h := mapXOF(s.Hash)
		return vetXofDST(h, []byte(s.DST))
	}
}

func (s *set) run(t *testing.T) {
	dst := s.dst()
	id := mapHash(s.Hash)

	for i, test := range s.Tests {
		t.Run(fmt.Sprintf("%s : Vector %d", s.Hash, i), func(t *testing.T) {
			v, err := test.decode()
			if err != nil {
				t.Fatalf("%d : %v", i, err)
			}

			dstPrime := dstPrime(dst)
			if !bytes.Equal(v.dstPrime, dstPrime) {
				t.Fatalf("%d : invalid DST prime.\ngot : %v\nwant: %v", i, dstPrime, v.dstPrime)
			}

			msgPrime := msgPrime(id, v.msg, dst, v.lenInBytes)
			if !bytes.Equal(v.msgPrime, msgPrime) {
				t.Fatalf("%d : invalid msg prime.", i)
			}

			var x []byte
			if isXMD(s.Hash) {
				x = ExpandXMD(mapXMD(s.Hash), v.msg, dst, v.lenInBytes)
			} else {
				x = ExpandXOF(mapXOF(s.Hash), v.msg, dst, v.lenInBytes)
			}

			if !bytes.Equal(v.uniformBytes, x) {
				t.Fatalf(
					"%d : invalid hash (length %d vs %d). expected %q, got %q",
					i,
					len(x),
					v.lenInBytes,
					v.uniformBytes,
					x,
				)
			}
		})
	}
}

func TestExpander_Vectors(t *testing.T) {
	if err := filepath.Walk("vectors",
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			file, errOpen := os.Open(path)
			if errOpen != nil {
				return errOpen
			}

			defer func(file *os.File) {
				err := file.Close()
				if err != nil {
				}
			}(file)

			val, errRead := ioutil.ReadAll(file)
			if errRead != nil {
				return errRead
			}

			var s set
			errJSON := json.Unmarshal(val, &s)
			if errJSON != nil {
				return errJSON
			}

			s.run(t)

			return nil
		}); err != nil {
		t.Fatalf("error opening set vectorStrings: %v", err)
	}
}
