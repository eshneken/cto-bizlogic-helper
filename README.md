# CTO ECAL Application Helper
The purpose of this module is to serve as an external helper for any business logic for the ECAL 3.0 application that is better served living outside the core VBCS Application.

The app requires a file named *config.json* to be present the same directory from which the app is run.  A sample file (with identifying credentials removed) looks like this:

```json
{
    "ServiceListenPort": "{{HTTP listen port for this instance}}",
    "ServiceUsername": "{{basic_auth_username_for_this_service}}",
    "ServicePassword": "{{basic_auth_password_for_this_service}}",
    "DBConnectString": "admin/{{password}}@{{DB SID}}",
    "ManagerHierarchyQuery": "SELECT UserEmail FROM %SCHEMA%.user1 u INNER JOIN %SCHEMA%.roletype rt ON u.rolename = rt.id WHERE rt.rolename = 'Manager' START WITH useremail = :1 CONNECT BY PRIOR useremail = manager",
    "InstanceEnvironments": "dev-preview,dev-stage,prod-stage,prod-live",
    "SchemaNames": "{{dev-preview schema name}},{dev-stage schema name}},{prod-stage schema name}},{prod-live schema name}}",
    "VBCSUsername": "{{vbcs_api_username}}",
    "VBCSPassword": "{{vbcs_api_password}}",
    "ECALBaseURL": "https://{{your_instance_name}}.integration.ocp.oraclecloud.com/ic/builder/design/ECAL/1.0/resources/data/",
}
```

This utility runs as an http server on a compute instance.  It listens, by default, on port 80 and requires the appropriate linux and cloud firewall/security list rules to allow incoming traffic to be created.  It also needs outbound access to the internet to access the service with ECALBaseURL

## Building the service from code
The following steps can be followed to build this service on Oracle Cloud Infrastructure (OCI):
1. Create a VCN with all related resources and update default security list to allow ingress access for TCP/80 and TCP/443
1. Create compute instance from "Oracle Developer" marketplace image
1. SSH into instance and open ingress for TCP/80 in linux firewall
    1. sudo firewall-cmd --zone=public --add-port=80/tcp --permanent
    1. sudo firewall-cmd --reload
1. Clone git repo (git clone {{this repo name}})
    1. git clone https://github.com/eshneken/cto-ecal-bizlogic
1. Download gjson dependency package 
    1. sudo go get -u github.com/tidwall/gjson
1. Download go-oracle dependency package 
    1. sudo go get -u gopkg.in/goracle.v2
1. Upload the ATP wallet file to the instance, copy to ~/wallet, and unzip the contents into that folder
    1. scp wallet.zip opc@{{ip_addr}}:/home/opc; [LOCAL]
    1. mkdir wallet; cd wallet; unzip ../wallet.zip; cd ..; rm wallet.zip
1. Configure TNS settings
    1. vi /home/opc/wallet/sqlnet.ora and replace ?/network/admin/ with /home/opc/wallet/
    1. vi ~/.bash_profile and add 'export TNS_ADMIN=/home/opc/wallet' to the bottom
    1. source ~/.bash_profile
1. Add a config.json file to the cto-ecal-bizlogic directory with the appropriate values
1. Build the package
    1. sudo go build
1. Run the service (make sure to preserve the environment with -E since TNS_ADMIN is sourced there)
    1. nohup sudo -E ./cto-ecal-bizlogic > server.out & 

## Usage
All endpoints require basic auth username & password

* getAccounts:  http://{{hostname}}/getAccounts?email={{email_addr}}&isManager={{true||false}}
* getManagerQuery:  http://{{hostname}}/getManagerQuery?managerEmail={{email_addr}}


## Principles for API Usage
* OIC REST API Reference:  https://docs.oracle.com/en/cloud/paas/identity-cloud/rest-api/
* Working with VBCS Business Object APIs:  https://docs.oracle.com/en/cloud/paas/app-builder-cloud/consume-rest/index.html

## Third Party Packages Used

 * Read-only JSON pathing support:  https://github.com/tidwall/gjson