//  Copyright (c) 2013 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var tiServer = flag.String("engine", "http://localhost:8093/", "URL to the query service(cbq-engine). By default, cbq connects to: http://localhost:8093\n\n Examples:\n\t cbq \n\t\t Connects to local query node. Same as: cbq -engine=http://localhost:8093\n\t cbq -engine=http://172.23.107.18:8093 \n\t\t Connects to query node at 172.23.107.18 Port 8093 \n\t cbq -engine=https://my.secure.node.com:8093 \n\t\t Connects to query node at my.secure.node.com:8093 using secure https protocol.\n")

var quietFlag = flag.Bool("quiet", false, "Enable/Disable startup connection message for the shell \n\t\t Default : false \n\t\t Possible Values : true/false \n")

func main() {
	flag.Parse()
	if strings.HasSuffix(*tiServer, "/") == false {
		*tiServer = *tiServer + "/"
	}
	if !*quietFlag {
		fmt.Printf("Couchbase query shell connected to %v . Type Ctrl-D to exit.\n", *tiServer)
	}
	HandleInteractiveMode(*tiServer, filepath.Base(os.Args[0]))
}

var transport = &http.Transport{MaxIdleConnsPerHost: 1}

// FIXME we really need a timeout here
var client = &http.Client{Transport: transport}

func execute_internal(tiServer, line string, w *os.File) error {

	url := tiServer + "query"
	if strings.HasPrefix(url, "https") {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	resp, err := client.Post(url, "text/plain", strings.NewReader(line))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(w, resp.Body)
	w.WriteString("\n")
	w.Sync()

	return nil
}
