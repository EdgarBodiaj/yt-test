package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/mux"
	libDatabox "github.com/me-box/lib-go-databox"
)

//default addresses to be used in testing mode
const testArbiterEndpoint = "tcp://127.0.0.1:4444"
const testStoreEndpoint = "tcp://127.0.0.1:5555"

var (
	cmdOut []byte
	er     error
	Indiv  Video
)

type Playlist struct {
	Item []Video `json:"entries"`
}

type Video struct {
	FullTitle   string   `json:"fulltitle"`
	Title       string   `json:"title"`
	AltTitle    string   `json:"alt_title"`
	Dislikes    int      `json:"dislike_count"`
	Views       int      `json:"view_count"`
	AvgRate     float64  `json:"average_rating"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Track       string   `json:"track"`
	ID          string   `json:"id"`
}

func main() {

	DataboxTestMode := os.Getenv("DATABOX_VERSION") == ""

	//The endpoints and routing for the UI
	router := mux.NewRouter()
	router.HandleFunc("/status", statusEndpoint).Methods("GET")
	router.HandleFunc("/ui/info", infoUser)
	router.PathPrefix("/ui").Handler(http.StripPrefix("/ui", http.FileServer(http.Dir("./static"))))
	setUpWebServer(DataboxTestMode, router, "8080")
}

func infoUser(w http.ResponseWriter, r *http.Request) {
	libDatabox.Info("Obtained auth")
	r.ParseForm()
	//Obtain user login details for their youtube account
	var username string
	var password string
	for k, v := range r.Form {
		if k == "email" {
			username = strings.Join(v, "")
		} else {
			password = strings.Join(v, "")
		}

	}
	fmt.Fprintf(w, "<h1>Authenticated and Working<h1>")
	go doDriverWork(username, password)

}

func doDriverWork(username string, password string) {
	DataboxTestMode := os.Getenv("DATABOX_VERSION") == ""
	libDatabox.Info("Starting ....")

	DataboxStoreEndpoint := "tcp://127.0.0.1:5555"
	var storeClient *libDatabox.CoreStoreClient

	if DataboxTestMode {
		DataboxStoreEndpoint = testStoreEndpoint
		ac, _ := libDatabox.NewArbiterClient("./", "./", testArbiterEndpoint)
		storeClient = libDatabox.NewCoreStoreClient(ac, "./", DataboxStoreEndpoint, false)
		//turn on debug output for the databox library
		libDatabox.OutputDebug(true)
	} else {
		DataboxStoreEndpoint = os.Getenv("DATABOX_ZMQ_ENDPOINT")
		storeClient = libDatabox.NewDefaultCoreStoreClient(DataboxStoreEndpoint)
	}

	libDatabox.Info("starting doDriverWork")

	//register our datasources
	//we only need to do this once at start up
	testDatasource := libDatabox.DataSourceMetadata{
		Description:    "Youtube History data",     //required
		ContentType:    libDatabox.ContentTypeJSON, //required
		Vendor:         "databox-test",             //required
		DataSourceType: "videoData",                //required
		DataSourceID:   "YoutubeHistory",           //required
		StoreType:      libDatabox.StoreTypeTSBlob, //required
		IsActuator:     false,
		IsFunc:         false,
	}
	arr := storeClient.RegisterDatasource(testDatasource)
	if arr != nil {
		libDatabox.Err("Error Registering Datasource " + arr.Error())
		return
	}
	libDatabox.Info("Registered Datasource")

	cmdName := "youtube-dl"
	tempUse := "-u " + username
	tempPas := "-p " + password
	cmdArgs := []string{tempUse, tempPas,
		"--skip-download",
		"--dump-single-json",
		"--playlist-items",
		"1-10",
		"https://www.youtube.com/feed/history"}

	//Create recent store
	var hOld Playlist
	for {
		//Create new var for incoming data
		var hNew Playlist
		if cmdOut, er = exec.Command(cmdName, cmdArgs[0], cmdArgs[1], cmdArgs[2], cmdArgs[3], cmdArgs[4], cmdArgs[5], cmdArgs[6]).Output(); er != nil {
			fmt.Println(er.Error())
			return
		}
		libDatabox.Info("Download Finished")
		err := json.Unmarshal(cmdOut, &hNew)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		//Check to see if the recent store is populated
		//If it has been populated, compare new items with the stored items
		if hOld.Item != nil {
			fmt.Println("New first item is: " + hNew.Item[0].Title)
			fmt.Println("Old first item is: " + hOld.Item[0].Title)
			for i := 0; i < len(hNew.Item); i++ {
				for j := 0; j < len(hOld.Item); j++ {
					//If a duplicate is found in the recent store, do not save item
					if hNew.Item[i].ID == hOld.Item[j].ID {
						break
					}
					//If no duplicates have been found in the store, save the item
					if j == len(hOld.Item)-1 {
						temp, tErr := json.Marshal(hNew.Item[i])
						if tErr != nil {
							fmt.Println(tErr.Error())
							return
						}
						aerr := storeClient.TSBlobJSON.Write("YoutubeHistory", temp)
						if aerr != nil {
							libDatabox.Err("Error Write Datasource " + aerr.Error())
						}
						//libDatabox.Info("Data written to store: " + string(temp))
						fmt.Println("Storing data")
						libDatabox.Info("Storing data")
					}
				}
			}
			//If its the first time the driver has been run, the recent store will be empty
			//Therefore store the current playlist items
		} else {
			fmt.Println("First case")
			for i := 0; i < len(hNew.Item); i++ {
				temp, tErr := json.Marshal(hNew.Item[i])
				if tErr != nil {
					fmt.Println("" + tErr.Error())
					return
				}
				fmt.Println(string(temp))
				libDatabox.Info(string(temp))
				aerr := storeClient.TSBlobJSON.Write("YoutubeHistory", temp)
				if aerr != nil {
					libDatabox.Err("Error Write Datasource " + aerr.Error())
				}
				//libDatabox.Info("Data written to store: " + string(temp))
				libDatabox.Info("Storing data")
			}
		}

		hOld = hNew

		time.Sleep(time.Second * 30)
		fmt.Println("New Cycle")
	}
}

func statusEndpoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("active\n"))
}

func setUpWebServer(testMode bool, r *mux.Router, port string) {

	//Start up a well behaved HTTP/S server for displying the UI

	srv := &http.Server{
		Addr:         ":" + port,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  30 * time.Second,
		Handler:      r,
	}
	if testMode {
		//set up an http server for testing
		libDatabox.Info("Waiting for http requests on port http://127.0.0.1" + srv.Addr + "/ui ....")
		log.Fatal(srv.ListenAndServe())
	} else {
		//configure tls
		tlsConfig := &tls.Config{
			PreferServerCipherSuites: true,
			CurvePreferences: []tls.CurveID{
				tls.CurveP256,
			},
		}

		srv.TLSConfig = tlsConfig

		libDatabox.Info("Waiting for https requests on port " + srv.Addr + " ....")
		log.Fatal(srv.ListenAndServeTLS(libDatabox.GetHttpsCredentials(), libDatabox.GetHttpsCredentials()))
	}
}
