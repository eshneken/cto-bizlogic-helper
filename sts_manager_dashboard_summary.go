//  STS Manager Dashboard Summary
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
// HTTP handler for the getSTSManagerDashboardSummaryHandler functionality
//
func getSTSManagerDashboardSummaryHandler(w http.ResponseWriter, r *http.Request) {
	// get query parameters
	query := r.URL.Query()
	managerEmail := query.Get("managerEmail")
	instanceEnv := query.Get("instanceEnvironment")

	// call the helper which does the data mashing
	result, err := getSTSManagerDashboardSummary(managerEmail, instanceEnv)
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
// Returns data to power the STS Manager Dashboard, specifically the Solution Engineer list.
// In addition to the manager email, the instanceEnvironment identifier (sts-dev-preview, sts-prod-live, etc)
// is required to key the name of the ATP schema to query
//
func getSTSManagerDashboardSummary(managerEmail string, instanceEnv string) (string, error) {
	// inject the correct schema name into the query
	if len(instanceEnv) < 1 {
		thisError := fmt.Sprintf("[%s] [%s] [%s] instanceEnvironment query parameter is invalid", time.Now().Format(time.RFC3339), instanceEnv, managerEmail)
		return "", errors.New(thisError)
	}

	// set the query
	var template = `
	   SELECT su.id as id,
		su.firstname || ' ' || su.lastname as name, 
		su.useremail as email, 
		p.id as pathId,
		p.pathname as pathName,
		(
			select count(pr.id) 
			from %SCHEMA%.STSAPathReq pr
			where pr.pathname = su.path
			) as totalTasksInPath,
		(
			select count(stat.id) 
			from %SCHEMA%.STSAUserStatus stat
			inner join %SCHEMA%.STSPath p on su.path = p.id
			inner join %SCHEMA%.STSTask t on stat.taskname = t.id
			inner join %SCHEMA%.STSAPathReq pr on t.id = pr.taskname and p.id = pr.pathname
			where stat.useremail = su.id and stat.taskstatus = 2
			) as tasksCompleted,
			(
			select count(stat.id) 
			from %SCHEMA%.STSAUserStatus stat
			inner join %SCHEMA%.STSPath p on su.path = p.id
			inner join %SCHEMA%.STSTask t on stat.taskname = t.id
			inner join %SCHEMA%.STSAPathReq pr on t.id = pr.taskname and p.id = pr.pathname
			where stat.useremail = su.id and stat.taskstatus = 3
			) as tasksValidated,
			TO_CHAR(NVL(
				(select max(lastupdatedate) from %SCHEMA%.STSAUserStatus stat where stat.useremail = su.id), 
				su.lastupdatedate), 'MM/DD/YYYY') 
			as lastActivity
		FROM %SCHEMA%.STSUser su
		INNER JOIN %SCHEMA%.STSPath p on su.path = p.id
		WHERE su.manager IN 
			(
			SELECT useremail 
			FROM %SCHEMA%.STSUser u 
			INNER JOIN %SCHEMA%.STSRole r 
			ON u.rolename = r.id WHERE r.rolename = 'Manager' 
			START WITH useremail = :1 
			CONNECT BY PRIOR useremail = manager
			)	
		ORDER BY name ASC
	`
	// replace the %SCHEMA% template with the correct schema name
	query := strings.ReplaceAll(template, "%SCHEMA%", SchemaMap[instanceEnv])

	// run the query
	rows, err := DBPool.Query(query, managerEmail)
	if err != nil {
		thisError := fmt.Sprintf("[%s] [%s] [%s] Error running query: %s", time.Now().Format(time.RFC3339), instanceEnv, managerEmail, err.Error())
		return "", errors.New(thisError)
	}
	defer rows.Close()

	// vars to hold row results
	var id, name, email, pathID, pathName, totalTasksInPath, tasksCompleted, tasksValidated, lastActivity string

	// step through each row returned and add to the query filter using the correct format
	result := ""
	count := 0
	for rows.Next() {
		err := rows.Scan(&id, &name, &email, &pathID, &pathName, &totalTasksInPath, &tasksCompleted, &tasksValidated, &lastActivity)
		if err != nil {
			thisError := fmt.Sprintf("[%s] [%s] [%s] Error scanning row: %s", time.Now().Format(time.RFC3339), instanceEnv, managerEmail, err.Error())
			return "", errors.New(thisError)
		}
		result += fmt.Sprintf("{\"id\": %s, \"name\": \"%s\", \"email\": \"%s\", \"pathId\": %s, \"pathName\": \"%s\", \"totalTasksInPath\": %s, \"tasksCompleted\": %s, \"tasksValidated\": %s, \"lastActivity\": \"%s\"},",
			id, name, email, pathID, pathName, totalTasksInPath, tasksCompleted, tasksValidated, lastActivity)
		count++
	}

	// string the trailing 'or' field if it exists
	result = strings.TrimSuffix(result, ",")

	//fmt.Printf("[%s] [%s] [%s] getSTSManagerDashboardSummary: results=%d\n", time.Now().Format(time.RFC3339), instanceEnv, managerEmail, count)
	return result, nil
}
