//  Health Handler
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"errors"
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
	healthErrors := "HEALTH_NOT_OK"

	schema := SchemaMap[GlobalConfig.ECALOpportunitySyncTarget]
	if len(schema) < 1 {
		thisError := fmt.Sprintf("[%s] Config healthcheck failed: Schema identifier [%s] not mappable", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget)
		fmt.Println(thisError)
		healthErrors = healthErrors + ":CONFIG"
		healthy = false
	}

	// make sure the database connection can be made
	rows1, err := DBPool.Query("SELECT SYSDATE FROM DUAL")
	if err != nil {
		thisError := fmt.Sprintf("[%s] DB healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
		fmt.Println(thisError)
		healthErrors = healthErrors + ":DB_ACCESS"
		healthy = false
	}
	defer rows1.Close()

	// make sure the ECAL account lookup table is populated
	count := 0
	rows2, err := DBPool.Query("SELECT count(*) FROM " + schema + ".LookupAccount")
	if err != nil {
		thisError := fmt.Sprintf("[%s] Account healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
		fmt.Println(thisError)
		healthErrors = healthErrors + ":ACCOUNT_DATA"
		healthy = false
	} else {
		defer rows2.Close()
		rows2.Next()
		err = rows2.Scan(&count)
		if err != nil || count == 0 {
			if err == nil {
				err = errors.New("LookupAccount has 0 rows")
			}
			thisError := fmt.Sprintf("[%s] Account healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
			fmt.Println(thisError)
			healthErrors = healthErrors + ":ACCOUNT_COUNT"
			healthy = false
		}
	}

	// make sure the ECAL opportunity lookup table is populated
	count = 0
	rows3, err := DBPool.Query("SELECT count(*) FROM " + schema + ".LookupOpportunity")
	if err != nil {
		thisError := fmt.Sprintf("[%s] Opportunity healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
		fmt.Println(thisError)
		healthErrors = healthErrors + ":OPPORTUNITY_DATA"
		healthy = false
	} else {
		defer rows3.Close()
		rows3.Next()
		err = rows3.Scan(&count)
		if err != nil || count == 0 {
			if err == nil {
				err = errors.New("LookupOpportunity has 0 rows")
			}
			thisError := fmt.Sprintf("[%s] Opportunity healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
			fmt.Println(thisError)
			healthErrors = healthErrors + ":OPPORTUNITY_COUNT"
			healthy = false
		}
	}

	// make sure identity filename exists and is readable
	_, err = ioutil.ReadFile(GlobalConfig.IdentityFilename)
	if err != nil {
		thisError := fmt.Sprintf("[%s] FILE healthcheck failed: %s", time.Now().Format(time.RFC3339), err.Error())
		fmt.Println(thisError)
		healthErrors = healthErrors + ":IDENTITY_DATA"
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
		fmt.Fprintf(w, healthErrors)
	}
}
