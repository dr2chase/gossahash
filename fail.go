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
	"math/rand"
	"os"
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

func doit(name string) bool {
	if os.Getenv("GOSSAHASH") == "" {
		// Default behavior is yes.
		return true
	}
	// Check the hash of the name against a partial input hash.
	// We use this feature to do a binary search within a
	// package to find a function that is incorrectly compiled.
	hstr := ""
	for _, b := range sha1.Sum([]byte(name)) {
		hstr += fmt.Sprintf("%08b", b)
	}
	if strings.HasSuffix(hstr, os.Getenv("GOSSAHASH")) {
		for i := 7 & rand.Int(); i >= 0; i-- {
			fmt.Printf("GOSSAHASH triggered %s\n", name)
		}
		return true
	}
	// Iteratively try additional hashes to allow tests for
	// multi-point failure.
	for i := 0; true; i++ {
		ev := fmt.Sprintf("GOSSAHASH%d", i)
		evv := os.Getenv(ev)
		if evv == "" {
			break
		}
		if strings.HasSuffix(hstr, evv) {
			for i := 7 & rand.Int(); i >= 0; i-- {
				fmt.Printf("%s triggered %s\n", ev, name)
			}
			return true
		}
	}
	return false
}

// test fails when "doit" is true for exactly 7 3-letter names.
// this simulates multiple triggers required for failure.
func test() {
	rand.Seed(time.Now().UnixNano())
	threeletters := 0
	for _, w := range names {
		if doit(w) && len(w) == 3 {
			threeletters++
		}
	}
	time.Sleep(1100 * time.Millisecond)
	if threeletters == 7 {
		fmt.Println("FAIL!")
		os.Exit(1)
	}
}
