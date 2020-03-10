//  LookupOpportunityHandler
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// OpportunityLookup represents an individual returned from the custom Aria export service
type OpportunityLookup struct {
	OppID          string `json:"opportunity_id"`
	OppName        string `json:"opportunity_name"`
	OppOwner       string `json:"opportunity_owner"`
	CloseDate      string `json:"close_date"`
	TCV            int    `json:"tcv_k"`
	WinProbability int    `json:"win_probabilty"`
}

// OpportunityLookupList represents an array of OpportunityLookup objcts
type OpportunityLookupList struct {
	Items []OpportunityLookup `json:"items"`
}

//
// HTTP handler that writes the contents of the identities file to the output
//
func postOpportunityLookupHandler(w http.ResponseWriter, r *http.Request) {
	// determine appropriate instance-environment
	query := r.URL.Query()
	schema := SchemaMap[query.Get("instanceEnvironment")]
	if len(schema) < 1 {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Query parameter instanceEnvironment ["+query.Get("instanceEnvironment")+"] is not valid.")
		fmt.Println("Query parameter instanceEnvironment [" + query.Get("instanceEnvironment") + "] is not valid.")
		return
	}

	// decode full opportunity list from response
	defer r.Body.Close()
	oppList := OpportunityLookupList{}
	json.NewDecoder(r.Body).Decode(&oppList)
	fmt.Printf("* Processing %d opportunities\n", len(oppList.Items))

	// start a DB transaction
	tx, err := DBPool.Begin()
	defer tx.Rollback()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error updating opportunities")
		fmt.Println("Error creating DB transaction: " + err.Error())
		return
	}

	// delete all data from LookupOpportunity table
	_, err = tx.Exec("DELETE FROM " + schema + ".LookupOpportunity")
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error updating opportunities")
		fmt.Println("Unable to delete from LookupOpportunity " + err.Error())
		return
	}

	// prepare insert statement
	insertStmt, err := tx.Prepare(
		"INSERT INTO " + schema + ".LookupOpportunity" +
			"(id, creationdate, lastupdatedate, createdby, lastupdatedby, abcschangenumber, " +
			"opportunityid, summary, salesrep, projectedarr, anticipatedclosedate, winprobability) " +
			"VALUES(:1, SYSDATE, SYSDATE, 'cto_bizlogic_helper', 'cto_bizlogic_helper', null, :2, :3, " +
			":4, :5, TO_DATE(:6, 'YYYY-MM-DD'), :7)")
	defer insertStmt.Close()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error updating opportunities")
		fmt.Println("Unable to prepare statement for insert " + err.Error())
		return
	}

	// prepare update statement
	updateStmt, err := tx.Prepare(
		"UPDATE " + schema + ".Opportunity SET" +
			" summary = :1, salesRep = :2, projectedARR = :3, anticipatedCloseDate = TO_DATE(:4, 'YYYY-MM-DD'), " +
			" winProbability = :5, lastUpdatedBy = 'cto_bizlogic_helper', lastUpdateDate = SYSDATE WHERE opportunityID = :6")
	defer updateStmt.Close()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error updating opportunities")
		fmt.Println("Unable to prepare statement for update " + err.Error())
		return
	}

	// iterate each opportunity
	for i, opp := range oppList.Items {

		// add opportunity to LookupOpportunity staging table
		_, err = insertStmt.Exec(i+1, opp.OppID, opp.OppName, opp.OppOwner, opp.TCV*1000, opp.CloseDate, opp.WinProbability)
		if err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, "Error updating opportunities")
			fmt.Printf("Unable to insert opportunity %s into LookupOpportunity: %s\n", opp.OppID, err.Error())
			return
		}

		// update existing Opportunity table with any updated data
		_, err = updateStmt.Exec(opp.OppName, opp.OppOwner, opp.TCV*1000, opp.CloseDate, opp.WinProbability, opp.OppID)
		if err != nil {
			w.WriteHeader(500)
			fmt.Fprintf(w, "Error updating opportunities")
			fmt.Printf("Unable to update opportunity %s in Opportunity: %s\n", opp.OppID, err.Error())
			return
		}

		i++
	}

	// complete the transaction
	err = tx.Commit()
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error updating opportunities")
		fmt.Println("Error committing transaction: " + err.Error())
		return
	}
}
