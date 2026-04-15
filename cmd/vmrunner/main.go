package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	seedFlag := flag.String("seed", "", "Seed string (hex or raw)")
	templateFlag := flag.String("template", "flag{<hmac>}", "Flag template with <hmac> placeholder")
	initFlag := flag.Bool("init-test", false, "Initialize a dummy seed file at /tmp/vmrunner/seed for testing")
	
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <question_no>\n", os.Args[0])
		flag.PrintDefaults()
	}
	
	flag.Parse()

	if *initFlag {
		doInit()
		return
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}
	questionNo := flag.Arg(0)

	seed := *seedFlag
	if seed == "" {
		// Try standard paths
		paths := []string{"/mnt/vmrunner/seed", "/tmp/vmrunner/seed", "./seed"}
		for _, p := range paths {
			if content, err := os.ReadFile(p); err == nil {
				seed = strings.TrimSpace(string(content))
				break
			}
		}
	}

	if seed == "" {
		seed = os.Getenv("VMRUNNER_SEED")
		if seed == "" {
			seed = "dev-seed"
		}
	}

	seedBytes, err := hex.DecodeString(seed)
	if err != nil {
		seedBytes = []byte(seed)
	}

	mac := hmac.New(sha256.New, seedBytes)
	mac.Write([]byte(questionNo))
	h := hex.EncodeToString(mac.Sum(nil))
	token := h[:5]

	if strings.Contains(*templateFlag, "<hmac>") {
		fmt.Println(strings.ReplaceAll(*templateFlag, "<hmac>", token))
	} else {
		fmt.Println(token)
	}
}

func doInit() {
	dir := "/tmp/vmrunner"
	_ = os.MkdirAll(dir, 0755)
	path := dir + "/seed"
	seed := "746573742d73656564" // "test-seed" in hex
	err := os.WriteFile(path, []byte(seed), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating dummy seed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Initialized dummy seed at %s\n", path)
	fmt.Println("You can now run: vmrunner 1")
}
