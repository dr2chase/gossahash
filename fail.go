// Copyright 2018 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"crypto/sha1"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

var names []string = []string{
	"preformulate",
	"tetracyn",
	"exptl",
	"extemporaneity",
	"presignalled",
	"licenced",
	"pyelographic",
	"riksmaal",
	"luminesce",
	"megawatt",
	"boeotus",
	"corporate",
	"saltine",
	"arsenide",
	"umbrellalike",
	"ecotonal",
	"cocoyam",
	"venetianed",
	"hiordis",
	"osteoma",
	"unshackle",
	"importability",
	"petrarchan",
	"elytron",
	"karbala",
	"haleakala",
	"unflirtatious",
	"emanuel",
	"catholicalness",
	"overawe",
	"pokable",
	"bacteroides",
	"amplifier",
	"paraphysate",
	"outseen",
	"wawa",
	"karoline",
	"excipule",
	"introductoriness",
	"grosgrained",
	"houdon",
	"interlocular",
	"toniest",
	"frozenly",
	"asexually",
	"ossification",
	"earthshine",
	"untransmuted",
	"karaism",
	"bond",
	"bituminize",
	"calycate",
	"codon",
	"sialkot",
	"ctesiphon",
	"griskin",
	"shiftily",
	"interdebate",
	"thistly",
	"effigiated",
	"misinference",
	"collinsville",
	"repatriate",
	"duplicatus",
	"nonordination",
	"geminated",
	"cauliflorous",
	"septembrist",
	"assertional",
	"incumber",
	"pedagogical",
	"sigher",
	"technicolor",
	"impugner",
	"anomalousness",
	"perhydrogenizing",
	"periastral",
	"lanchow",
	"machineless",
	"djinny",
	"ruga",
	"cerebroid",
	"genip",
	"environs",
	"muticate",
	"adamic",
	"indivisibility",
	"crissa",
	"conjunctive",
	"nonsculptured",
	"keble",
	"subverter",
	"gelignite",
	"stilettoed",
	"gratulated",
	"guanase",
	"proselytise",
	"orthrus",
	"excursionary",
	"ellipsoidal",
	"ant",
	"bat",
	"cat",
	"dog",
	"emu",
	"fox",
	"gnu",
	"hen",
}

type hashAndMask struct {
	// a hash h matches if (h^hash)&mask == 0
	hash uint64
	mask uint64
	name string // base name, or base name + "0", "1", etc.
}

type writeSyncer interface {
	io.Writer
	Sync() error
}

type HashDebug struct {
	name     string        // base name of the flag/variable.
	matches  []hashAndMask // A hash matches if one of these matches.
	excludes []hashAndMask // explicitly excluded hash suffixes
	logfile  writeSyncer
	yes, no  bool
}

func toHashAndMask(s, varname string) hashAndMask {
	l := len(s)
	if l > 64 {
		s = s[l-64:]
		l = 64
	}
	m := ^(^uint64(0) << l)
	h, err := strconv.ParseUint(s, 2, 64)
	if err != nil {
		panic(fmt.Errorf("Could not parse %s (=%s) as a binary number", varname, s))
	}

	return hashAndMask{name: varname, hash: h, mask: m}
}

// NewHashDebug returns a new hash-debug tester for the
// environment variable ev.  If ev is not set, it returns
// nil, allowing a lightweight check for normal-case behavior.
func NewHashDebug(ev, s string) *HashDebug {
	fmt.Printf("NewHashDebug(%s,%s)\n", ev, s)
	if s == "" {
		return nil
	}

	hd := &HashDebug{name: ev}
	switch s[0] {
	case 'y', 'Y':
		hd.yes = true
		return hd
	case 'n', 'N':
		hd.no = true
		return hd
	}
	var ss []string
	var sc string // current
	for _, c := range s {
		switch c {
		// Ignore leading 'v' from bisect also.
		case '/', '+', ',', ' ', '\t', '-', 'v':
			if len(sc) > 0 {
				ss = append(ss, sc)
				sc = ""
			}
		default:
			sc += string(c)
			continue
		}
		if c == '-' {
			sc = "-"
		}
	}
	if len(sc) > 0 {
		ss = append(ss, sc)
		sc = ""
	}

	// hash searches may use additional EVs with 0, 1, 2, ... suffixes.
	i := 0
	for _, s := range ss {
		if s == "" {
			if i != 0 || len(ss) > 1 && ss[1] != "" || len(ss) > 2 {
				panic(fmt.Errorf("Empty hash match string for %s should be first (and only) one", ev))
			}
			// Special case of should match everything.
			hd.matches = append(hd.matches, toHashAndMask("0", fmt.Sprintf("%s0", ev)))
			hd.matches = append(hd.matches, toHashAndMask("1", fmt.Sprintf("%s1", ev)))
			break
		}
		if s[0] == '-' {
			hd.excludes = append(hd.excludes, toHashAndMask(s[1:], fmt.Sprintf("%s%d", "HASH_EXCLUDE", i)))
		} else {
			if i == 0 {
				hd.matches = append(hd.matches, toHashAndMask(s, fmt.Sprintf("%s", ev)))
			} else {
				hd.matches = append(hd.matches, toHashAndMask(s, fmt.Sprintf("%s%d", ev, i-1)))
			}
			i++
		}
	}
	return hd
}

func hashOf(pkgAndName string, param uint64) uint64 {
	hbytes := sha1.Sum([]byte(pkgAndName))
	hash := uint64(hbytes[7])<<56 + uint64(hbytes[6])<<48 +
		uint64(hbytes[5])<<40 + uint64(hbytes[4])<<32 +
		uint64(hbytes[3])<<24 + uint64(hbytes[2])<<16 +
		uint64(hbytes[1])<<8 + uint64(hbytes[0])

	if param != 0 {
		// Because param is probably a line number, probably near zero,
		// hash it up a little bit, but even so only the lower-order bits
		// likely matter because search focuses on those.
		p0 := param + uint64(hbytes[9]) + uint64(hbytes[10])<<8 +
			uint64(hbytes[11])<<16 + uint64(hbytes[12])<<24

		p1 := param + uint64(hbytes[13]) + uint64(hbytes[14])<<8 +
			uint64(hbytes[15])<<16 + uint64(hbytes[16])<<24

		param += p0 * p1
		param ^= param>>17 ^ param<<47
	}

	return hash ^ param
}

// DebugHashMatch returns true if either the variable used to create d is
// unset, or if its value is y, or if it is a suffix of the base-two
// representation of the hash of pkgAndName.  If the variable is not nil,
// then a true result is accompanied by stylized output to d.logfile, which
// is used for automated bug search.
func (d *HashDebug) DebugHashMatch(pkgAndName string) bool {
	return d.DebugHashMatchParam(pkgAndName, 0)
}

// DebugHashMatchParam returns true if either the variable used to create d is
// unset, or if its value is y, or if it is a suffix of the base-two
// representation of the hash of pkgAndName and param. If the variable is not
// nil, then a true result is accompanied by stylized output to d.logfile,
// which is used for automated bug search.
func (d *HashDebug) DebugHashMatchParam(pkgAndName string, param uint64) bool {
	if d == nil {
		return true
	}
	if d.no {
		return false
	}
	hash := hashOf(pkgAndName, param)

	for _, m := range d.excludes {
		if (m.hash^hash)&m.mask == 0 {
			return false
		}
	}

	if len(d.matches) == 0 || d.yes {
		xstr := fmt.Sprintf("0x%x", hash)
		d.logDebugHashMatch(d.name, pkgAndName, xstr, param)
		return true
	}

	for _, m := range d.matches {
		if (m.hash^hash)&m.mask == 0 {
			xstr := fmt.Sprintf("0x%x", hash)
			d.logDebugHashMatch(m.name, pkgAndName, xstr, param)
			return true
		}
	}
	return false
}

func (d *HashDebug) logDebugHashMatch(varname, name, hstr string, param uint64) {
	file := d.logfile
	if file == nil {
		if tmpfile := os.Getenv("GSHS_LOGFILE"); tmpfile != "" {
			var err error
			file, err = os.OpenFile(tmpfile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				panic(fmt.Errorf("could not open hash-testing logfile %s", tmpfile))
			}
		}
		if file == nil {
			file = os.Stdout
		}
		d.logfile = file
	}
	if len(hstr) > 32 {
		hstr = hstr[len(hstr)-32:]
	}
	// External tools depend on this string
	if param == 0 {
		// loopvarhash1 triggered ./a/a.go:11:6 001001011000010011100011
		if !bisectSyntax {
			fmt.Fprintf(file, "%s triggered %s %s\n", varname, name, hstr)
		}
		// ./a/a.go:11:6 [bisect-match 0x800ddd09be2584e3]
		fmt.Fprintf(file, "%s [bisect-match %s]\n", name, hstr)
	} else {
		if !bisectSyntax {
			fmt.Fprintf(file, "%s triggered %s:%d %s\n", varname, name, param, hstr)
		}
		fmt.Fprintf(file, "%s:%d [bisect-match %s]\n", name, param, hstr)
	}
}

var doit = newDoit
var hd *HashDebug

func newDoit(name string, param int) bool {
	return hd.DebugHashMatchParam(name, uint64(param))
}

// test fails when "doit" is true for 4 or more 3-letter names.
// this simulates multiple triggers required for failure.
func test() {

	gcd := os.Getenv("GOCOMPILEDEBUG")
	li := strings.LastIndex(gcd, "=")
	hd = NewHashDebug(hash_ev_name, gcd[li+1:])
	rand.Seed(time.Now().UnixNano())
	threeletters := 0
	for i, w := range names {
		if doit(w, i) && len(w) == 3 {
			threeletters++
		}
	}
	time.Sleep(50 * time.Millisecond)

	if threeletters >= 4 {
		fmt.Println("FAIL!")
		os.Exit(1)
	}
}
