//  ECAL Artifact Query
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

//
// HTTP handler for the getECALDataQueryHandler functionality
//
func getECALArtifactQueryHandler(w http.ResponseWriter, r *http.Request) {
	// get query parameters
	query := r.URL.Query()
	instanceEnv := query.Get("instanceEnvironment")

	// call the helper which does the data mashing
	result, err := getECALArtifactQuery(instanceEnv)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error in input parameters or processing; please contact your service administrator")
		fmt.Printf("***ERROR: %s\n", string(err.Error()))
		return
	}

	// format the result as json
	json := fmt.Sprintf("{\"items\": [%s]}", result)

	// write result to output stream
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(json))
}

//
// Returns artifacs to power the ECAL artifact curation admin function.
// The instanceEnvironment identifier (sts-dev-preview, sts-prod-live, etc) is required to key the name of the ATP schema to query
//
func getECALArtifactQuery(instanceEnv string) (string, error) {
	// inject the correct schema name into the query
	if len(instanceEnv) < 1 {
		thisError := fmt.Sprintf("[%s] [%s] getECALArtifactQuery: instanceEnvironment query parameter is invalid", time.Now().Format(time.RFC3339), instanceEnv)
		return "", errors.New(thisError)
	}

	// set the core query
	var template = `select a.id, a.accountname account, o.opportunityid oppid, sf.name solutionfoucs, ra.name type, a.lastupdatedby ce, to_char(a.lastupdatedate, 'MM-DD-YYYY') uploaded, a.location url
	from %SCHEMA%.opportunityartifacts a
	inner join %SCHEMA%.opportunity o on a.opportunity = o.id
	inner join %SCHEMA%.account a on o.account = a.id
	inner join %SCHEMA%.opportunitysolutionfocu osf on osf.opportunity = o.id
	inner join %SCHEMA%.solutionfocus sf on sf.id = osf.solutionfocus
	inner join %SCHEMA%.requiredartifacts ra on a.artifact = ra.id
	where round(cast(SYSDATE as DATE) - cast(a.lastupdatedate as date)) < 180
	order by a.lastupdatedate desc`

	var jsonResultTemplate = `{"id":"%s","account":"%s","opp_id":"%s","solution_focus":"%s","artifact_type":"%s","ce":"%s","uploaded":"%s","location":"%s"},`

	// replace the %SCHEMA% template with the correct schema name
	query := strings.ReplaceAll(template, "%SCHEMA%", SchemaMap[instanceEnv])
	//fmt.Println(query)

	// run the query
	rows, err := DBPool.Query(query)
	if err != nil {
		thisError := fmt.Sprintf("[%s] [%s] getECALArtifactQuery: Error running query: %s", time.Now().Format(time.RFC3339), instanceEnv, err.Error())
		return "", errors.New(thisError)
	}
	defer rows.Close()

	// vars to hold row results
	var id, account, oppid, solutionfocus, artifactType, ce, uploaded, location string

	// step through each row returned and add to the query filter using the correct format
	result := ""
	count := 0
	for rows.Next() {
		err := rows.Scan(&id, &account, &oppid, &solutionfocus, &artifactType, &ce, &uploaded, &location)
		if err != nil {
			thisError := fmt.Sprintf("[%s] [%s] getECALArtifactQuery: Error scanning row: %s", time.Now().Format(time.RFC3339), instanceEnv, err.Error())
			return "", errors.New(thisError)
		}

		result += fmt.Sprintf(jsonResultTemplate,
			id, account, oppid, solutionfocus, artifactType, ce, uploaded, location)
		count++
	}

	// string the trailing 'or' field if it exists
	result = strings.TrimSuffix(result, ",")

	//fmt.Printf("[%s] [%s] getECALArtifactQuery: results=%d\n", time.Now().Format(time.RFC3339), instanceEnv, count)
	return result, nil
}
