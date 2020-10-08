//  ECAL Data Query
//	CTO Business Logic Helpers
//	Ed Shnekendorf, 2020, https://github.com/eshneken/cto-bizlogic-helper

package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

//
// HTTP handler for the getECALDataQueryHandler functionality
//
func getECALDataQueryHandler(w http.ResponseWriter, r *http.Request) {
	// get query parameters
	query := r.URL.Query()
	instanceEnv := query.Get("instanceEnvironment")

	// call the helper which does the data mashing
	result, err := getECALDataQuery(instanceEnv)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintf(w, "Error in input parameters or processing; please contact your service administrator")
		logOutput(logError, "ecal_data_query", string(err.Error()))
		return
	}

	// format the result as json
	json := fmt.Sprintf("{\"items\": [%s]}", result)

	// write result to output stream
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, string(json))
}

//
// Returns data to power the ECAL application.  Specifically returns a list of accounts that should be presented to the user of the app.
// The instanceEnvironment identifier (sts-dev-preview, sts-prod-live, etc) is required to key the name of the ATP schema to query
//
func getECALDataQuery(instanceEnv string) (string, error) {
	// inject the correct schema name into the query
	if len(instanceEnv) < 1 {
		thisError := fmt.Sprintf("instanceEnvironment query parameter is invalid (%s)", instanceEnv)
		return "", errors.New(thisError)
	}

	// set the core query
	var template = `with function calculateColor(adopter varchar2, implementer varchar2, logarch number, archdiag number, bom number, poc number, pocstatus varchar2, secsignoff number, 
		techsignoff number, consplan number, consplansignoff number, ccInvolved number, ccSar number) return char is
totalCount number := 10;
score number := 0;
begin
-- Existing business applications or process identified with consumption potential
score := score + 1;
-- Identity customer implementer/adoption owner
if length(implementer) > 1 and length(adopter) > 1 then
score := score + 1;
end if;
-- Solution Reviewed (Technical and Functional Design)
if logarch = 1 and archdiag = 1 then
score := score + 1;
end if;
-- Initial BOM Identified
if bom = 1 then
score := score + 1;
end if;
-- POC Complete if POC required
if (poc = 0) or (poc = 1 and pocstatus = 'Completed') then
score := score + 1;
end if;
-- Final Solution Architecture & BOM Completed
if logarch = 1 and bom = 1 and techsignoff = 1 then
score := score + 1;
end if;
-- Complete Security Review
if secsignoff = 1 then
score := score + 1;
end if;
-- Customer Agrees to Consumption plan
if consplan = 1 and consplansignoff = 1 then
score := score + 1;
end if;
-- Technical Signoff w/ date/email
if techsignoff = 1 then
score := score + 1;
end if;
-- SAR Complete if C@C deal
if (ccInvolved = 0) or (ccInvolved = 1 and ccSar = 1) then
score := score + 1;
end if;
-- return color code for score
if score <= 4 then
return 'R';
elsif score > 2 and score < totalCount then
return 'Y';
else
return 'G';
end if;
end;
		select  
			distinct(o.id) as ecal_workload_id,
			a.id as ecal_account_id,
			o.opportunityid as opportunity_id,
			nvl(w.workloadtype, 'None') as workload_type,
			w.workloadidentifier as workload_identifier,
			a.accountname as account_name,
			a.cimid as cim_id,
			o.summary as workload_summary,    
			calculateColor(
				th.adoptionowneremail,
				th.implementeremail,
				(select ora1.done from %SCHEMA%.opportunityrequiredarti ora1 inner join %SCHEMA%.requiredartifacts ra1 ON ora1.requiredartifact = ra1.id where o.id = ora1.opportunity and ra1.name = 'Logical Architecture'),
				(select ora2.done from %SCHEMA%.opportunityrequiredarti ora2 inner join %SCHEMA%.requiredartifacts ra2 ON ora2.requiredartifact = ra2.id where o.id = ora2.opportunity and ra2.name = 'Architecture Diagram'),
				(select ora3.done from %SCHEMA%.opportunityrequiredarti ora3 inner join %SCHEMA%.requiredartifacts ra3 ON ora3.requiredartifact = ra3.id where o.id = ora3.opportunity and ra3.name = 'Bill of Materials'),
				nvl(th.pocrequired, 0),
				nvl(th.pocstatus, 'Not Started'),
				nvl(th.securitysignoffdone, 0), 
				nvl(th.technicalsignoffdone, 0), 
				(select ora4.done from %SCHEMA%.opportunityrequiredarti ora4 inner join %SCHEMA%.requiredartifacts ra4 ON ora4.requiredartifact = ra4.id where o.id = ora4.opportunity and ra4.name = 'Consumption Plan'), 
				nvl(th.consumptionplansignoff, 0),
				nvl(th.cloudatcustomerinvolved, 0), 
				nvl(th.cloudatcustomersardone, 0))
			as color,
			nvl((select stage FROM %SCHEMA%.EcalStage where id = o.lateststagedone), 'None') as latest_ecal_stage_done,
			nvl(a.currentcsaexecuted, 0) as csa_executed,
			o.technicallead as tech_lead,
			u.manager as tech_manager,
			nvl(th.pocrequired, 0) as poc_required,
			to_char(th.pocenddate, 'MM-DD-YYYY') as poc_enddate,
			nvl(th.pocstatus, 'Not Started') as poc_status,
			nvl(th.pocresolution, 'None') as poc_resolution,
			nvl(th.securitysignoffdone, 0) as security_signoff,
			nvl(th.technicalsignoffdone, 0) as technical_signoff,
			nvl(th.consumptionplansignoff, 0) as cons_plan_signoff,
			nvl(th.cloudatcustomerinvolved, 0) as cc_involved,
			nvl(th.cloudatcustomersardone,0) as cc_done,
			nvl(th.technicalblockers, 0) as tech_blockers,
			nvl(th.commercialblockers, 0) as commercial_blockers,
			nvl(th.coronavirusimpact, 0) as covid_impact,
			nvl(th.oracleconsultingengaged, 0) as ocs_engaged,
			nvl(th.expansion, 0) as expansion,
			translate(th.technicaldecisionmakern, chr(10)||chr(11)||chr(13)||chr(34), '  ') as tech_decider,
			to_char(th.technicalsignoffdate, 'MM-DD-YYYY') as tech_signoff_date,
			translate(th.migrationrunby, chr(10)||chr(11)||chr(13)||chr(34), '  ') as migration_by,
			translate(th.tigerseemail, chr(10)||chr(11)||chr(13)||chr(34), '  ') as tiger_se_email,
			translate(th.partnername, chr(10)||chr(11)||chr(13)||chr(34), '  ') as partner_name,
			translate(th.workloadprogressionstage, chr(10)||chr(11)||chr(13)||chr(34), '  ') as workload_progression,
			translate(th.adoptionowneremail, chr(10)||chr(11)||chr(13)||chr(34), '  ') as adopter_email,
			translate(th.adoptionownernametitle, chr(10)||chr(11)||chr(13)||chr(34), '  ') as adopter_name,
			translate(th.implementeremail, chr(10)||chr(11)||chr(13)||chr(34), '  ') as implementer_email,
			translate(th.implementernametitle, chr(10)||chr(11)||chr(13)||chr(34), '  ') as implementer_name,
			(select ora1.done
			FROM %SCHEMA%.OpportunityRequiredArti ora1
			INNER JOIN %SCHEMA%.RequiredArtifacts ra1 ON ora1.requiredartifact = ra1.id
			where o.id = ora1.opportunity and ra1.name = 'Logical Architecture') as future_state_complete,
			nvl(((select ora2.done
			FROM %SCHEMA%.OpportunityRequiredArti ora2
			INNER JOIN %SCHEMA%.RequiredArtifacts ra2 ON ora2.requiredartifact = ra2.id
			where o.id = ora2.opportunity and ra2.name = 'Architecture Diagram') intersect (select ora21.done
			FROM %SCHEMA%.OpportunityRequiredArti ora21
			INNER JOIN %SCHEMA%.RequiredArtifacts ra21 ON ora21.requiredartifact = ra21.id
			where o.id = ora21.opportunity and ra21.name = 'Inventory Spreadsheet')), 0) as current_state_complete,
			(select ora3.done
			FROM %SCHEMA%.OpportunityRequiredArti ora3
			INNER JOIN %SCHEMA%.RequiredArtifacts ra3 ON ora3.requiredartifact = ra3.id
			where o.id = ora3.opportunity and ra3.name = 'Consumption Plan') as consumption_plan_complete,
			translate(nvl(os.status, 'No Status Entered'), chr(10)||chr(11)||chr(13)||chr(34), '  ') as latest_status,
			to_char(os.creationdate, 'MM-DD-YYYY') as latest_status_date,
			os.lastupdatedby as latest_status_author,
			-- 08-OCT-2020 PBOCCHIO START
			, o.lateststagedone
			, (select decode(sum(ora.done),0, min(s.phase), max(s.phase)) phase
				from %SCHEMA%.opportunityrequiredarti ora
				inner join %SCHEMA%.requiredartifacts ra on ra.id = ora.requiredartifact
				inner join %SCHEMA%.ecalstage s on ra.ecalstage = s.id 
				inner join %SCHEMA%.ecalphase p on s.phase = p.id
				where ora.opportunity = o.id 
				group by ora.opportunity ) as currentphase
			, (select listagg(u.useremail,':') within group (order by DECODE(u.useremail,o.technicallead,1,0) desc, u.useremail) from %SCHEMA%.useraccount ua inner join %SCHEMA%.user1 u on u.id = ua.user1 where ua.account = o.account  ) resourceslist
			, (select listagg(DECODE(u.useremail,o.technicallead,'Y','N'),':') within group (order by DECODE(u.useremail,o.technicallead,1,0) desc, u.useremail) from %SCHEMA%.useraccount ua inner join %SCHEMA%.user1 u on u.id = ua.user1 where ua.account = o.account ) techleadlist
			-- 08-OCT-2020 PBOCCHIO END
		FROM %SCHEMA%.Opportunity o
		INNER JOIN %SCHEMA%.Account a ON a.id = o.account
		LEFT OUTER JOIN %SCHEMA%.OpportunityTechHealth th ON th.opportunity = o.id
		LEFT OUTER JOIN %SCHEMA%.OpportunityWorkload w ON w.opportunity = o.id
		LEFT OUTER JOIN %SCHEMA%.User1 u ON o.technicallead = u.useremail
		LEFT OUTER JOIN %SCHEMA%.OpportunityStatus os ON o.id = os.opportunity
		and not exists (select 1 FROM %SCHEMA%.OpportunityStatus os1 where os1.opportunity = o.id and os1.creationdate > os.creationdate)`

	var jsonResultTemplate = `{"ecal_workload_id":"%s","ecal_account_id":"%s","opportunity_id":"%s","workload_type":"%s","workload_identifier":"%s","account_name":"%s","cim_id":"%s","workload_summary":"%s","color":"%s","latest_ecal_stage_done": "%s","csa_executed":"%s","tech_lead":"%s","tech_manager":"%s","poc_required":"%s","poc_enddate":"%s","poc_status":"%s","poc_resolution":"%s","security_signoff":"%s","technical_signoff":"%s","cons_plan_signoff": "%s","cc_involved":"%s","cc_done":"%s","tech_blockers":"%s","commercial_blockers":"%s","covid_impact":"%s","ocs_engaged":"%s","expansion":"%s","tech_decider":"%s","tech_signoff_date":"%s","migration_by": "%s","tiger_se_email": "%s","partner_name":"%s","workload_progression":"%s","adopter_email":"%s","adopter_name":"%s","implementer_email":"%s","implementer_name":"%s","future_state_complete":"%s","current_state_complete":"%s","consumption_plan_complete":"%s","latest_status":"%s","latest_status_date":"%s","latest_status_author":"%s","latest_stage_done":"%s","current_phase":"%s","resource_list":"%s","techlead_list":"%s"},`

	// replace the %SCHEMA% template with the correct schema name
	query := strings.ReplaceAll(template, "%SCHEMA%", SchemaMap[instanceEnv])
	//fmt.Println(query)

	// run the query
	rows, err := DBPool.Query(query)
	if err != nil {
		thisError := fmt.Sprintf("Error running query (%s): %s", instanceEnv, err.Error())
		return "", errors.New(thisError)
	}
	defer rows.Close()

	// vars to hold row results
	var ecalWorkloadID, ecalAccountID, opportunityID, workloadType, workloadIdentifier, accountName, cimID, workloadSummary, color, latestECALStageDone string
	var csaExecuted, techLead, techManager, pocRequired, pocEndDate, pocStatus, pocResolution, securitySignoff, technicalSignoff, consPlanSignoff string
	var ccInvolved, ccDone, techBlockers, commercialBlockers, covidImpact, ocsEngaged, expansion, techDecider, techSignoffDate, migrationBy, tigerSeEmail string
	var partnerName, workloadProgression, adopterEmail, adopterName, implementerEmail, implementerName, futureStateComplete, currentStateComplete, consumptionPlanComplete, latestStatus, latestStatusDate, latestStatusAuthor string
	var latestStageDone, currentPhase, resourceList, techLeadList string

	// step through each row returned and add to the query filter using the correct format
	result := ""
	count := 0
	for rows.Next() {
		err := rows.Scan(&ecalWorkloadID, &ecalAccountID, &opportunityID, &workloadType, &workloadIdentifier, &accountName, &cimID, &workloadSummary, &color, &latestECALStageDone,
			&csaExecuted, &techLead, &techManager, &pocRequired, &pocEndDate, &pocStatus, &pocResolution, &securitySignoff, &technicalSignoff, &consPlanSignoff,
			&ccInvolved, &ccDone, &techBlockers, &commercialBlockers, &covidImpact, &ocsEngaged, &expansion, &techDecider, &techSignoffDate, &migrationBy, &tigerSeEmail,
			&partnerName, &workloadProgression, &adopterEmail, &adopterName, &implementerEmail, &implementerName, &futureStateComplete, &currentStateComplete, &consumptionPlanComplete, &latestStatus, &latestStatusDate, &latestStatusAuthor,
			&latestStageDone, &currentPhase, &resourceList, &techLeadList)
		if err != nil {
			thisError := fmt.Sprintf("Error scanning row (%s): %s", instanceEnv, err.Error())
			return "", errors.New(thisError)
		}

		result += fmt.Sprintf(jsonResultTemplate,
			ecalWorkloadID, ecalAccountID, opportunityID, workloadType, workloadIdentifier, accountName, cimID, workloadSummary, color, latestECALStageDone,
			csaExecuted, techLead, techManager, pocRequired, pocEndDate, pocStatus, pocResolution, securitySignoff, technicalSignoff, consPlanSignoff,
			ccInvolved, ccDone, techBlockers, commercialBlockers, covidImpact, ocsEngaged, expansion, techDecider, techSignoffDate, migrationBy, tigerSeEmail,
			partnerName, workloadProgression, adopterEmail, adopterName, implementerEmail, implementerName, futureStateComplete, currentStateComplete, consumptionPlanComplete, latestStatus, latestStatusDate, latestStatusAuthor,
			latestStageDone, currentPhase, resourceList, techLeadList)
		count++
	}

	// string the trailing 'or' field if it exists
	result = strings.TrimSuffix(result, ",")

	return result, nil
}
