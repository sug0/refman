package main

import (
    "io"
    "os"
    "log"
    "fmt"
    "flag"
    "bufio"
    "errors"
    "strings"
    "runtime"
    "io/ioutil"
    "path/filepath"

    "github.com/nickng/bibtex"

    pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"

    "github.com/blevesearch/bleve"
    "github.com/blevesearch/bleve/search/highlight/highlighter/ansi"
)

type Document struct {
    Ref *bibtex.BibTex
    Txt string
}

// this is set upon program init
var (
    workDir   string
    indexPath string
)

// cmd line flags
var (
    verbose    bool
    verbose2   bool
    pdfFile    string
    bibtexFile string
)

// logger stuff
var (
    logger *log.Logger
)

func init() {
    // parse flags
    flag.BoolVar(&verbose, "v", false, "Print logs to stderr.")
    flag.BoolVar(&verbose2, "vv", false, "Even more verbosity.")
    flag.StringVar(&pdfFile, "pdf", "", "The PDF file to parse.")
    flag.StringVar(&bibtexFile, "bibtex", "", "The BibTeX file to parse.")
    flag.Parse()

    // configure logger
    if verbose || verbose2 {
        logger = log.New(os.Stderr, os.Args[0]+": ", log.LstdFlags)
    } else {
        logger = log.New(ioutil.Discard, os.Args[0]+": ", log.LstdFlags)
    }

    // create and set workdir
    envDir := os.Getenv("REFMAN_WORKDIR")
    if envDir != "" {
        workDir = envDir
        goto skipDefaults
    }

    switch runtime.GOOS {
    default:
        home, err := os.UserHomeDir()
        if err != nil {
            panic(err)
        }
        workDir = filepath.Join(home, ".local/share/refman")
    case "windows":
        workDir = filepath.Join(os.Getenv("APPDATA"), "refman")
    }

skipDefaults:
    logger.Printf("Using working directory: %s\n", workDir)
    if err := os.MkdirAll(workDir, 0777); err != nil {
        if !errors.Is(err, os.ErrExist) {
            panic(err)
        }
    }
    indexPath = filepath.Join(workDir, "index.bleve")
}

func main() {
    index, err := openIndex()
    if err != nil {
        panic(err)
    }
    defer index.Close()

    // add a new entry
    if pdfFile != "" {
        err = addEntry(index)
        if err != nil {
            panic(err)
        }
        return
    }

    // perform search
    args := flag.Args()
    if len(args) == 0 {
        logger.Println("No query given, leaving.")
        return
    }

    s := strings.ReplaceAll(strings.Join(args, " "), "~", "-")
    queryString := bleve.NewQueryStringQuery(s)
    searchRequest := bleve.NewSearchRequest(queryString)
    if verbose2 {
        searchRequest.Highlight = bleve.NewHighlightWithStyle(ansi.Name)
    }

    result, err := index.Search(searchRequest)
    if err != nil {
        panic(err)
    }
    fmt.Println(result)
}

func addEntry(index bleve.Index) error {
    absPath, err := filepath.Abs(pdfFile)
    if err != nil {
        return err
    }
    doc, err := parseDocument()
    if err != nil {
        return err
    }
    logger.Println("Updating index file with new entry.")
    return index.Index(absPath, doc)
}

func parseDocument() (*Document, error) {
    // parse pdf text
    logger.Printf("Opening PDF file: %s\n", pdfFile)
    ctx, err := pdfcpuapi.ReadContextFile(pdfFile)
    if err != nil {
        return nil, err
    }

    logger.Printf("Parsing PDF file: %s\n", pdfFile)
    err = ctx.EnsurePageCount()
	if err != nil {
		return nil, err
	}

    var txt strings.Builder

    for i := 1; i <= ctx.PageCount; i++ {
        plainText, err := ctx.ExtractPageContent(i)
        if err != nil {
            return nil, err
        }
        if plainText == nil {
            continue
        }
        _, err = io.Copy(&txt, plainText)
        if err != nil {
            return nil, err
        }
    }

    // parse bibtex
    var ref *bibtex.BibTex
    if bibtexFile != "" {
        logger.Printf("Attempting to parse BibTeX file: %s\n", bibtexFile)
        f, err := os.Open(bibtexFile)
        if err != nil {
            return nil, err
        }
        defer f.Close()

        ref, err = bibtex.Parse(bufio.NewReader(f))
        if err != nil {
            return nil, err
        }
    }

    return &Document{Ref: ref, Txt: txt.String()}, nil
}

func openIndex() (bleve.Index, error) {
    logger.Printf("Opening index file: %s\n", indexPath)
    index, err := bleve.Open(indexPath)
    if err != nil {
        logger.Println("Open failed, creating index.")
        mapping := bleve.NewIndexMapping()
        index, err = bleve.New(indexPath, mapping)
        if err != nil {
            return nil, err
        }
    }
    logger.Println("Index loaded successfully.")
    return index, nil
}
