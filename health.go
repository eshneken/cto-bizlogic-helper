//  Health Handler
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

//
// HTTP handler for health checks
//
func healthHandler(w http.ResponseWriter, r *http.Request) {
	healthy := true

	// make sure the database connection can be made
	rows, err := DBPool.Query("SELECT SYSDATE FROM DUAL")
	if err != nil {
		thisError := fmt.Sprintf("[%s] DB healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
		fmt.Println(thisError)
		healthy = false
	}
	defer rows.Close()

	// make sure identity filename exists and is readable
	_, err = ioutil.ReadFile(GlobalConfig.IdentityFilename)
	if err != nil {
		thisError := fmt.Sprintf("[%s] FILE healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
		fmt.Println(thisError)
		healthy = false
	}

	// write appropriate response code based on health condition
	if healthy {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		fmt.Fprintf(w, "HEALTH_OK")
	} else {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(500)
		fmt.Fprintf(w, "HEALTH_NOT_OK")
	}
}
