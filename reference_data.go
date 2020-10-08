//  PostReferenceData Handler
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
)

// postion options
const first = "first"
const middle = "middle"
const last = "last"
const reprocess = "reprocess"

// data type options
const identity = "identity"
const opportunity = "opportunity"
const account = "account"

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
		message := fmt.Sprintf("Missing or invalid position parameter: %s", position)
		logOutput(logError, "reference_data", message)
		return
	}

	dataType := query.Get("type")
	if dataType != identity && dataType != opportunity && dataType != account {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Missing or invalid type query string parameter")
		message := fmt.Sprintf("Missing or invalid type parameter: %s", dataType)
		logOutput(logError, "reference_data", message)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		message := fmt.Sprintf("Unable to read body: %s", err.Error())
		logOutput(logError, "reference_data", message)
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
			message := fmt.Sprintf("Error writing to file in 'first' position (%s): %s", dataType, err.Error())
			logOutput(logError, "reference_data", message)
			fmt.Fprintf(w, "Processing Error")
			w.WriteHeader(500)
		}
		message := fmt.Sprintf("[START Collecting Data (%s)", dataType)
		logOutput(logInfo, "reference_data", message)
	} else {
		// all other normative positions (middle & last) require appending to the existing file
		// we don't do this when reprocessing; we assume a complete file is already on disk
		if position == middle || position == last {
			file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0700)
			if err != nil {
				message := fmt.Sprintf("Error writing datatype %s to file %s in %s position: %s",
					dataType, filename, position, err.Error())
				logOutput(logError, "reference_data", message)
				fmt.Fprintf(w, "Processing Error")
				w.WriteHeader(500)
			}

			if _, err := file.Write(body); err != nil {
				file.Close()
				message := fmt.Sprintf("Error writing datatype %s to file %s in %s position: %s",
					dataType, filename, position, err.Error())
				logOutput(logError, "reference_data", message)
				fmt.Fprintf(w, "Processing Error")
				w.WriteHeader(500)
			}
			file.Close()
		}

		// in last position we need to kick off processing.  same applies to reprocessing.
		if position == last || position == reprocess {
			message := fmt.Sprintf("DONE Collecting Data (%s)", dataType)
			logOutput(logInfo, "reference_data", message)

			// process identity data in separate goroutine
			if dataType == identity {
				message = fmt.Sprintf("Handing off to identity processor (%s)", dataType)
				logOutput(logInfo, "reference_data", message)
				go processIdentity(filename)
			}

			// process opportunity data in separate goroutine
			if dataType == opportunity {
				message = fmt.Sprintf("Handing off to opportunity processor (%s)", dataType)
				logOutput(logInfo, "reference_data", message)
				go processOpportunity(filename)
			}

			// process account data in separate goroutine
			if dataType == account {
				message = fmt.Sprintf("Handing off to account processor (%s)", dataType)
				logOutput(logInfo, "reference_data", message)
				go processAccount(filename)
			}
		}
	}
}
