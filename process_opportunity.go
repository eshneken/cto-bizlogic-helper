//  ProcessOpportunity
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper
//
// refer to https://golang.org/src/database/sql/sql_test.go for golang SQL samples

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// OpportunityLookup represents an individual returned from the custom Aria export service
type OpportunityLookup struct {
	OppID          string `json:"opportunity_id"`
	OppName        string `json:"opportunity_name"`
	OppOwner       string `json:"opportunity_owner"`
	TerritoryOwner string `json:"territory_owner"`
	OppStatus      string `json:"opportunity_status"`
	CloseDate      string `json:"close_date"`
	CustomerName   string `json:"customer_name"`
	ARR            string `json:"pipeline_k"`
	TCV            string `json:"tcv_k"`
	WinProbability string `json:"win_probability"`
	IntegrationID  string `json:"opty_int_id"`
	RegistryID     string `json:"registry_id"`
	CimID          string `json:"cim_id"`
}

//
// Process opportunities from JSON file to LookupOpportunity table
//
func processOpportunity(filename string) {

	// determine appropriate instance-environment based on the value of the config.json setting
	schema := SchemaMap[GlobalConfig.ECALOpportunitySyncTarget]
	if len(schema) < 1 {
		fmt.Printf("[%s] processOpportunity: Schema for [%s] not valid\n",
			time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget)
		return
	}

	// open file for reading
	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("[%s] processOpportunity: Error opening file [%s]: %s\n",
			time.Now().Format(time.RFC3339), filename, err.Error())
		return
	}
	defer file.Close()

	// seek 10 bytes (chars) to advance past {"items":
	_, err = file.Seek(10, io.SeekStart)
	if err != nil {
		fmt.Printf("[%s] processOpportunity: Error advancing file stream to position 10: %s\n",
			time.Now().Format(time.RFC3339), err.Error())
		return
	}

	// decode full opportunity list from response
	decoder := json.NewDecoder(file)
	fmt.Printf("[%s] [%s] processOpportunity: START Processing opportunities\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget)

	// start a DB transaction
	tx, err := DBPool.Begin()
	defer tx.Rollback()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Error creating DB transaction: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// delete all data from LookupOpportunity table
	_, err = tx.Exec("DELETE FROM " + schema + ".LookupOpportunity")
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Unable to delete from LookupOpportunity: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// prepare insert statement
	insertStmt, err := tx.Prepare(
		"INSERT INTO " + schema + ".LookupOpportunity" +
			"(id, creationdate, lastupdatedate, createdby, lastupdatedby, abcschangenumber, " +
			"opportunityid, summary, salesrep, projectedarr, anticipatedclosedate, winprobability, " +
			"projectedtcv, integrationid, registryid, cimid, opportunitystatus, customername, territoryowner) " +
			"VALUES(:1, SYSDATE, SYSDATE, 'cto_bizlogic_helper', 'cto_bizlogic_helper', null, :2, :3, " +
			":4, :5, TO_DATE(:6, 'YYYY-MM-DD'), :7, :8, :9, :10, :11, :12, :13, :14)")
	defer insertStmt.Close()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Unable to prepare statement for insert: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// prepare update statement
	updateStmt, err := tx.Prepare(
		"UPDATE " + schema + ".Opportunity SET" +
			" summary = :1, salesRep = :2, projectedARR = :3, projectedTCV = :4, opportunityStatus = :5, anticipatedCloseDate = TO_DATE(:6, 'YYYY-MM-DD'), " +
			" winProbability = :7, lastUpdatedBy = 'cto_bizlogic_helper', lastUpdateDate = SYSDATE WHERE opportunityID = :8")
	defer updateStmt.Close()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Unable to prepare statement for update: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// consume the opening array brace
	_, err = decoder.Token()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Error decoding opening array token: %s\n",
			time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// iterate each opportunity
	insertedOpps := 0
	counter := 1
	for decoder.More() {
		// decode next record
		var opp OpportunityLookup
		err := decoder.Decode(&opp)
		if err != nil {
			fmt.Printf("[%s] [%s] processOpportunity: Error decoding person %d: %s\n",
				time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, counter, err.Error())
			return
		}

		// convert strings to floats
		tcv, _ := strconv.ParseFloat(opp.TCV, 64)
		arr, _ := strconv.ParseFloat(opp.ARR, 64)
		winProbability, _ := strconv.ParseInt(opp.WinProbability, 10, 64)

		// truncate timestamps
		opp.CloseDate = strings.TrimSuffix(strings.Split(opp.CloseDate, "T")[0], "T")

		// add opportunity to LookupOpportunity staging table
		// only put opportunities in 'Open' or 'Won' state into the lookup table
		if opp.OppStatus == "Open" || opp.OppStatus == "Won" {
			_, err = insertStmt.Exec(counter, opp.OppID, opp.OppName, opp.OppOwner, arr*1000, opp.CloseDate, winProbability,
				tcv*1000, opp.IntegrationID, opp.RegistryID, opp.CimID, opp.OppStatus, opp.CustomerName, opp.TerritoryOwner)
			if err != nil {
				fmt.Printf("[%s] [%s] processOpportunity: Unable to insert opportunity %s into LookupOpportunity: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, opp.OppID, err.Error())
				return
			}
			insertedOpps++
		}

		// update existing Opportunity table with any updated data.  We do this regardless of opportunity status since
		// this will allow us to 'close' previously open opportunities
		_, err = updateStmt.Exec(opp.OppName, opp.OppOwner, arr*1000, tcv*1000, opp.OppStatus, opp.CloseDate, winProbability, opp.OppID)
		if err != nil {
			fmt.Printf("[%s] [%s] processOpportunity: Unable to update opportunity %s in Opportunity: %s\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, opp.OppID, err.Error())
			return
		}

		counter++
	}

	// consume the closing array brace
	_, err = decoder.Token()
	if err != nil {
		fmt.Printf("[%s] [%s] processIdentity: Error decoding closing array token: %s\n",
			time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	// complete the transaction
	err = tx.Commit()
	if err != nil {
		fmt.Printf("[%s] [%s] processOpportunity: Error committing transaction: %s\n", time.Now().Format(time.RFC3339),
			GlobalConfig.ECALOpportunitySyncTarget, err.Error())
		return
	}

	fmt.Printf("[%s] [%s] processOpportunity: DONE Processing %d opportunities with %d in Open/Won state\n", time.Now().Format(time.RFC3339), GlobalConfig.ECALOpportunitySyncTarget, counter, insertedOpps)
}
