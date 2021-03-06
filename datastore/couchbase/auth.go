//  Copyright (c) 2017 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package couchbase

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"

	"github.com/couchbase/cbauth"
	"github.com/couchbase/query/auth"
	"github.com/couchbase/query/errors"
	"github.com/couchbase/query/logging"
)

func opIsUnimplemented(namespace, object string, requested auth.Privilege) bool {
	if namespace == "#system" {
		// For system monitoring tables INSERT and UPDATE are not supported.
		if object == "prepareds" || object == "completed_requests" || object == "active_requests" {
			if requested == auth.PRIV_QUERY_UPDATE || requested == auth.PRIV_QUERY_INSERT {
				return true
			}
			return false
		}
		// For other system buckets, INSERT/UPDATE/DELETE are not supported.
		if requested == auth.PRIV_QUERY_UPDATE || requested == auth.PRIV_QUERY_INSERT || requested == auth.PRIV_QUERY_DELETE {
			return true
		}
		return false
	}
	return false
}

func privilegeString(namespace, target, obj string, requested auth.Privilege) (string, error) {
	var permission string
	switch requested {
	case auth.PRIV_WRITE:
		permission = join5Strings("cluster.", obj, "[", target, "].data.docs!write")
	case auth.PRIV_READ:
		permission = join5Strings("cluster.", obj, "[", target, "].data.docs!read")
	case auth.PRIV_SYSTEM_READ:
		permission = "cluster.n1ql.meta!read"
	case auth.PRIV_SECURITY_READ:
		permission = "cluster.admin.security!read"
	case auth.PRIV_SECURITY_WRITE:
		permission = "cluster.admin.security!write"
	case auth.PRIV_QUERY_SELECT:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.select!execute")
	case auth.PRIV_QUERY_UPDATE:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.update!execute")
	case auth.PRIV_QUERY_INSERT:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.insert!execute")
	case auth.PRIV_QUERY_DELETE:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.delete!execute")
	case auth.PRIV_QUERY_BUILD_INDEX:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.index!build")
	case auth.PRIV_QUERY_CREATE_INDEX:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.index!create")
	case auth.PRIV_QUERY_ALTER_INDEX:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.index!alter")
	case auth.PRIV_QUERY_DROP_INDEX:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.index!drop")
	case auth.PRIV_QUERY_LIST_INDEX:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.index!list")
	case auth.PRIV_QUERY_EXTERNAL_ACCESS:
		permission = "cluster.n1ql.curl!execute"
	case auth.PRIV_QUERY_MANAGE_FUNCTIONS:
		permission = "cluster.n1ql.udf!manage"
	case auth.PRIV_QUERY_EXECUTE_FUNCTIONS:
		permission = "cluster.n1ql.udf!execute"
	case auth.PRIV_QUERY_MANAGE_SCOPE_FUNCTIONS:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.udf!manage")
	case auth.PRIV_QUERY_EXECUTE_SCOPE_FUNCTIONS:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.udf!execute")
	case auth.PRIV_QUERY_MANAGE_FUNCTIONS_EXTERNAL:
		permission = "cluster.n1ql.udf_external!manage"
	case auth.PRIV_QUERY_EXECUTE_FUNCTIONS_EXTERNAL:
		permission = "cluster.n1ql.udf_external!execute"
	case auth.PRIV_QUERY_MANAGE_SCOPE_FUNCTIONS_EXTERNAL:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.udf_external!manage")
	case auth.PRIV_QUERY_EXECUTE_SCOPE_FUNCTIONS_EXTERNAL:
		permission = join5Strings("cluster.", obj, "[", target, "].n1ql.udf_external!execute")
	case auth.PRIV_QUERY_BUCKET_ADMIN:
		permission = join3Strings("cluster.bucket[", target, "]!manage")
	case auth.PRIV_QUERY_STATS:
		permission = "cluster.admin.internal.stats!read"
	default:
		return "", fmt.Errorf("Invalid Privileges")
	}
	return permission, nil
}

func doAuthByCreds(creds cbauth.Creds, namespace string, target string, obj string, requested auth.Privilege) (bool, error) {
	permission, err := privilegeString(namespace, target, obj, requested)
	if err != nil {
		return false, err
	}
	authResult, err := creds.IsAllowed(permission)
	if err != nil || authResult == false {
		return false, err
	}

	return true, nil
}

type authSource interface {
	adminIsOpen() bool
	auth(user, pwd string) (cbauth.Creds, error)
	authWebCreds(req *http.Request) (cbauth.Creds, error)
}

// splits the target into namespace and couchbase target (bucket, or collection path separated by :)
// determines the type of object to check (bucket, scope, collection)
func namespaceKeyspaceTypeFromPrivPair(pair auth.PrivilegePair) (string, string, string) {
	var bytes []byte
	var namespace string
	var obj string

	i := strings.IndexByte(pair.Target, ':')
	if i < 0 {
		namespace = "default"
		bytes = []byte(pair.Target)
	} else {
		namespace = pair.Target[:i]
		if i < len(pair.Target)-1 {
			bytes = []byte(pair.Target[i+1:])
		}
	}

	// count the items
	cnt := 0

	// cbAuth separates target collection path objects with ':', not '.' as N1QL does
	for i := 0; i < len(bytes); i++ {
		if bytes[i] == '.' {
			bytes[i] = ':'
			cnt++
		}
	}

	switch cnt {
	case 0:
		obj = "bucket"
	case 1:
		obj = "scope"
	default:
		obj = "collection"
	}
	return namespace, string(bytes), obj
}

// Try to get privsSought privileges from the availableCredentials credentials.
// Return the privileges that were not granted.
func authAgainstCreds(as authSource, privsSought []auth.PrivilegePair, availableCredentials []cbauth.Creds) ([]auth.PrivilegePair, error) {
	deniedPrivs := make([]auth.PrivilegePair, 0, len(privsSought))
	for _, pair := range privsSought {
		namespace, keyspace, obj := namespaceKeyspaceTypeFromPrivPair(pair)
		privilege := pair.Priv

		thisPrivGranted := false

		if namespace == "#system" && keyspace == "nodes" && privilege == auth.PRIV_SYSTEM_READ && as.adminIsOpen() {
			// The system:nodes table follows the underlying ns_server API.
			// If all tables have passwords, the API requires credentials.
			// But if any don't, the API is open to read.
			continue
		}

		if opIsUnimplemented(namespace, keyspace, privilege) {
			// Trivially grant permission for unimplemented operations.
			// Error reporting will be handled by the execution layer.
			continue
		}

		// Check requested privilege against the list of credentials.
		for _, creds := range availableCredentials {
			authResult, err := doAuthByCreds(creds, namespace, keyspace, obj, privilege)

			if err != nil {
				return nil, err
			}

			// Auth succeeded
			if authResult == true {
				thisPrivGranted = true
				break
			}
		}

		// This privilege can not be granted by these credentials.
		if !thisPrivGranted {
			deniedPrivs = append(deniedPrivs, pair)
		}
	}
	return deniedPrivs, nil
}

// Determine the set of keyspaces referenced in the list of privileges, and derive
// credentials for them with empty passwords. This corresponds to the case of access users that were
// created for passwordless buckets at upgrate time.

// TODO: ditch this code, when we require that users created for old style passwordless buckets have a password
// as legacy code, we choose not to make it collection aware
func deriveDefaultCredentials(as authSource, privs []auth.PrivilegePair) ([]cbauth.Creds, auth.AuthenticatedUsers) {
	keyspaces := make(map[string]bool, len(privs))
	for _, pair := range privs {
		keyspace := pair.Target
		if keyspace == "" {
			continue
		}
		if strings.Contains(keyspace, ":") {
			q := strings.Split(keyspace, ":")
			keyspace = q[1]
		}
		keyspaces[keyspace] = true
	}

	creds := make([]cbauth.Creds, 0, len(keyspaces))
	authUsers := make(auth.AuthenticatedUsers, 0, len(keyspaces))
	password := ""
	for username := range keyspaces {
		user, err := as.auth(username, password)
		if err == nil {
			creds = append(creds, user)
			authUsers = append(authUsers, username)
		}
	}
	return creds, authUsers
}

func userKeyString(c cbauth.Creds) string {
	return join3Strings(c.Domain(), ":", c.Name())
}

// could be variadic functions, but we are looking for speed
func join3Strings(s1, s2, s3 string) string {
	var buff bytes.Buffer

	buff.WriteString(s1)
	buff.WriteString(s2)
	buff.WriteString(s3)
	return buff.String()
}

func join5Strings(s1, s2, s3, s4, s5 string) string {
	var buff bytes.Buffer

	buff.WriteString(s1)
	buff.WriteString(s2)
	buff.WriteString(s3)
	buff.WriteString(s4)
	buff.WriteString(s5)
	return buff.String()
}

func isClientCertPresent(req *http.Request) bool {
	return req != nil && req.TLS != nil && req.TLS.PeerCertificates != nil
}

func cbAuthorize(s authSource, privileges *auth.Privileges, credentials *auth.Credentials) (auth.AuthenticatedUsers, errors.Error) {

	// Create credentials - list and authenticated users to use for auth calls
	if credentials.Users == nil {
		credentials.Users = make(map[string]string, 0)
	}
	authenticatedUsers := make(auth.AuthenticatedUsers, 0, len(credentials.Users))
	credentialsList := make([]cbauth.Creds, 0, 2)

	// Query allows 4 kinds of authorization methods -
	// 		 1. Basic Auth
	//		 2. Auth Header/Token
	//		 3. Certificates
	//		 4. Creds query parameter

	// The call to AuthWebCreds takes care of the first 3 checks.
	// This needs to be performed first. The following is taken care of by authwebcreds -

	// X509 - mandatory
	// Only certs can be used to authorize. No other method.

	// X509 - disable
	// 		1. Certificates,Basic Auth or Auth header can be used to authorize

	// X509 - enable
	// 		1. Cert needs to be used to authorize if present
	//		2. If cert not present then we need to use other methods
	//		   (partially done by auth web creds)

	req := credentials.HttpRequest
	if req != nil {
		creds, err := s.authWebCreds(req)
		if err == nil {
			credentialsList = append(credentialsList, creds)
			authenticatedUsers = append(authenticatedUsers, userKeyString(creds))
		} else if err.Error() == "No web credentials found in request." {
			// Do nothing.
		} else {

			clientAuthType, err1 := cbauth.GetClientCertAuthType()
			if err1 != nil {
				return nil, errors.NewDatastoreAuthorizationError(err1)
			}

			// If enable or mandatory and client cert is present, and you see an error
			// Then return the error.
			if clientAuthType != tls.NoClientCert && isClientCertPresent(req) {
				return nil, errors.NewDatastoreAuthorizationError(err)
			}
		}
	}

	// Either error isn't nil and mode is disable and error is nil

	// If we could not successfully authorize above, and
	//  - if x509 is disable or
	//  - if x509 is enable and cert is not present
	// we need to check if creds can be used to authorize

	// request could be nil - in which case we handle credentials only

	if len(credentialsList) == 0 {
		for username, password := range credentials.Users {
			var un string
			userCreds := strings.Split(username, ":")
			if len(userCreds) == 1 {
				un = userCreds[0]
			} else {
				un = userCreds[1]
			}

			logging.Debugf(" Credentials for user <ud>%v</ud>", un)
			creds, err := s.auth(un, password)
			if err != nil {
				logging.Debugf("Unable to authorize <ud>%s</ud>. Error - %v", username, err)
				return nil, errors.NewDatastoreAuthorizationError(err)
			} else {
				credentialsList = append(credentialsList, creds)
				if un != "" {
					authenticatedUsers = append(authenticatedUsers, userKeyString(creds))
				}
			}
		}
	}

	// No privileges to check? Done.
	if privileges == nil || len(privileges.List) == 0 {
		return authenticatedUsers, nil
	}

	// Check every requested privilege against the credentials list.
	// if the authentication fails for any of the requested privileges return an error
	remainingPrivileges, err := authAgainstCreds(s, privileges.List, credentialsList)

	if err != nil {
		return nil, errors.NewDatastoreAuthorizationError(err)
	}

	if len(remainingPrivileges) == 0 {
		// Everything is authorized. Success!
		return authenticatedUsers, nil
	}

	// Derive possible default credentials from remaining privileges.
	defaultCredentials, defaultUsers := deriveDefaultCredentials(s, remainingPrivileges)
	authenticatedUsers = append(authenticatedUsers, defaultUsers...)
	deniedPrivileges, err := authAgainstCreds(s, remainingPrivileges, defaultCredentials)

	if err != nil {
		return nil, errors.NewDatastoreAuthorizationError(err)
	}

	if len(deniedPrivileges) == 0 {
		// Authorized using defaults.
		return authenticatedUsers, nil
	}

	msg := messageForDeniedPrivilege(deniedPrivileges[0])
	return nil, errors.NewDatastoreInsufficientCredentials(msg)
}
