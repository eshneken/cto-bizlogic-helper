//  PostReferenceData Handler
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

// postion options
const first = "first"
const middle = "middle"
const last = "last"
const reprocess = "reprocess"

// data type options
const identity = "identity"
const opportunity = "opportunity"

//
// HTTP handler that takes chunks of external reference data, combines into files, and calls the appropriate
// handler to process
//
func postReferenceDataHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	position := query.Get("position")
	if position != first && position != middle && position != last && position != reprocess {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Missing or invalid position query string parameter")
		fmt.Printf("[%s] postReferenceDataHandler: Missing or invalid position parameter: %s\n",
			time.Now().Format(time.RFC3339), position)
		return
	}

	dataType := query.Get("type")
	if dataType != identity && dataType != opportunity {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Missing or invalid type query string parameter")
		fmt.Printf("[%s] postReferenceDataHandler: Missing or invalid type parameter: %s\n",
			time.Now().Format(time.RFC3339), dataType)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("[%s] postReferenceDataHandler: Unable to read body: %s\n",
			time.Now().Format(time.RFC3339), err.Error())
		fmt.Fprintf(w, "Unable to read body")
		w.WriteHeader(500)
		return
	}

	//fmt.Printf("[%s] postReferenceDataHandler: Payload size: %d\n",time.Now().Format(time.RFC3339), len(body))

	// write data to filesystem
	filename := dataType + ".json"
	if position == first {
		// first position requires opening a new file and writing to it.  if an old file exists it is overwritten
		err = ioutil.WriteFile(filename, body, 0700)
		if err != nil {
			fmt.Printf("[%s] [%s] postReferenceDataHandler: Error writing to file in 'first' position: %s\n",
				time.Now().Format(time.RFC3339), dataType, err.Error())
			fmt.Fprintf(w, "Processing Error")
			w.WriteHeader(500)
		}
		fmt.Printf("[%s] [%s] postReferenceDataHandler: START Collecting Data\n",
			time.Now().Format(time.RFC3339), dataType)
	} else {
		// all other normative positions (middle & last) require appending to the existing file
		// we don't do this when reprocessing; we assume a complete file is already on disk
		if position == middle || position == last {
			file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0700)
			if err != nil {
				fmt.Printf("[%s] [%s] postReferenceDataHandler: Error writing to file [%s] in [%s] position: %s\n",
					time.Now().Format(time.RFC3339), dataType, filename, position, err.Error())
				fmt.Fprintf(w, "Processing Error")
				w.WriteHeader(500)
			}

			if _, err := file.Write(body); err != nil {
				file.Close()
				fmt.Printf("[%s] [%s] postReferenceDataHandler: Error writing to file [%s] in [%s] position: %s\n",
					time.Now().Format(time.RFC3339), dataType, filename, position, err.Error())
				fmt.Fprintf(w, "Processing Error")
				w.WriteHeader(500)
			}
			file.Close()
		}

		// in last position we need to kick off processing.  same applies to reprocessing.
		if position == last || position == reprocess {
			fmt.Printf("[%s] [%s] postReferenceDataHandler: DONE Collecting Data\n",
				time.Now().Format(time.RFC3339), dataType)

			// process identity data in separate goroutine
			if dataType == identity {
				fmt.Printf("[%s] [%s] postReferenceDataHandler: Handing off to identity processor\n",
					time.Now().Format(time.RFC3339), dataType)
				go processIdentity(filename)
			}

			// process opportunity data in separate goroutine
			if dataType == opportunity {
				fmt.Printf("[%s] [%s] postReferenceDataHandler: Handing off to opportunity processor\n",
					time.Now().Format(time.RFC3339), dataType)
				go processOpportunity(filename)
			}
		}
	}
}
