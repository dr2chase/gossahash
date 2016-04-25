package main

import (
	"crypto/sha1"
	"fmt"
	"os"
	"strings"
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
		fmt.Printf("GOSSAHASH triggered %s\n", name)
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
			fmt.Printf("%s triggered %s\n", ev, name)
			return true
		}
	}
	return false
}

// test fails when "doit" is true for exactly 7 3-letter names.
// this simulates multiple triggers required for failure.
func test() {
	threeletters := 0
	for _, w := range names {
		if doit(w) && len(w) == 3 {
			threeletters++
		}
	}
	if threeletters == 7 {
		fmt.Println("FAIL!")
		os.Exit(1)
	}
}
