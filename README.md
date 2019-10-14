# CTO ECAL Application Helper
The purpose of this module is to serve as an external helper for any business logic for the ECAL 3.0 application that is better served living outside the core VBCS Application.

The app requires a file named *config.json* to be present the same directory from which the app is run.  A sample file (with identifying credentials removed) looks like this:

```json
{
    "ServiceListenPort": "{{HTTP listen port for this instance}}",
    "ServiceUsername": "{{basic_auth_username_for_this_service}}",
    "ServicePassword": "{{basic_auth_password_for_this_service}}",
    "DBConnectString": "admin/{{password}}@{{DB SID}}",
    "ManagerHierarchyQuery": "SELECT UserEmail FROM {{schema}}.user1 u INNER JOIN {{schema}}.roletype rt ON u.rolename = rt.id WHERE rt.rolename = 'Manager' START WITH useremail = :1 CONNECT BY PRIOR useremail = manager",
    "VBCSUsername": "{{vbcs_api_username}}",
    "VBCSPassword": "{{vbcs_api_password}}",
    "ECALBaseURL": "https://{{your_instance_name}}.integration.ocp.oraclecloud.com/ic/builder/design/ECAL/1.0/resources/data/",
}
```

This utility runs as an http server on a compute instance.  It listens, by default, on port 80 and requires the appropriate linux and cloud firewall/security list rules to allow incoming traffic to be created.  It also needs outbound access to the internet to access the service with ECALBaseURL

## Building the service from code
The following steps can be followed to build this service on Oracle Cloud Infrastructure (OCI):
1. Create a VCN with security list ingress access for TCP/80
2. Create compute instance from "Oracle Developer" marketplace image
3. SSH into instance and open ingress for TCP/80 in linux firewall
    a. sudo firewall-cmd --zone=public --add-port=80/tcp --permanent
    b. sudo firewall-cmd --reload
4. Clone git repo (git clone {{this repo name}})
    a. git clone https://github.com/eshneken/cto-ecal-bizlogic
5. Download gjson dependency package 
    a. sudo go get -u github.com/tidwall/gjson
6. Download go-oracle dependency package 
    a. sudo go get -u gopkg.in/goracle.v2
7. Upload the ATP wallet file to the instance, copy to ~/wallet, and unzip the contents into that folder
    a. scp wallet.zip opc@{{ip_addr}}:/home/opc; [LOCAL]
    b. mkdir wallet; cd wallet; unzip ../wallet.zip; cd ..; rm wallet.zip
8. Configure TNS settings
    a. vi /home/opc/wallet/sqlnet.ora and replace ?/network/admin/ with /home/opc/wallet/
    b. vi ~/.bash_profile and add 'export TNS_ADMIN=/home/opc/wallet' to the bottom
    c. source ~/.bash_profile
9. Add a config.json file to the cto-ecal-bizlogic directory with the appropriate values
10. Build the package
    a. sudo go build
11. Run the service (make sure to preserve the environment with -E since TNS_ADMIN is sourced there)
    a. nohup sudo -E ./cto-ecal-bizlogic > server.out & 

## Usage
All endpoints require basic auth username & password

* getAccounts:  http://{{hostname}}/getAccounts?email={{email_addr}}&isManager={{true||false}}
* getManagerQuery:  http://{{hostname}}/getManagerQuery?managerEmail={{email_addr}}


## Principles for API Usage
* OIC REST API Reference:  https://docs.oracle.com/en/cloud/paas/identity-cloud/rest-api/
* Working with VBCS Business Object APIs:  https://docs.oracle.com/en/cloud/paas/app-builder-cloud/consume-rest/index.html

## Third Party Packages Used

 * Read-only JSON pathing support:  https://github.com/tidwall/gjson