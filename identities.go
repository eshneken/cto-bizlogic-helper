//  Identities Handler
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

//
// HTTP handler that writes the contents of the identities file to the output
//
func postIdentitiesQueryHandler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Println(outputHTTPError("postIdentitiesQueryHandler", err, nil))
		w.WriteHeader(500)
		return
	}
	// write identities to filesystem
	err = ioutil.WriteFile(GlobalConfig.IdentityFilename, body, 0700)
	if err != nil {
		fmt.Println(outputHTTPError("postIdentitiesQueryHandler", err, nil))
		w.WriteHeader(500)
	}
}

//
// HTTP handler that writes the contents of the identities file to the output
//
func getIdentitiesQueryHandler(w http.ResponseWriter, r *http.Request) {
	// open identities JSON file from filesystem
	data, err := ioutil.ReadFile(GlobalConfig.IdentityFilename)
	if err != nil {
		fmt.Println(outputHTTPError("getIdentitiesQueryHandler", err, nil))
		w.WriteHeader(500)
		return
	}

	// write result to output stream
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(data))
}
