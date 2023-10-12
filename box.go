package box

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"go.k6.io/k6/js/modules"
)

func init() {
	modules.Register("k6/x/box", new(RootModule))
}

type RootModule struct{}

type Box struct {
	vu modules.VU
}

var (
	_ modules.Module   = &RootModule{}
	_ modules.Instance = &Box{}
)

func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	return &Box{
		vu: vu,
	}
}

func (box *Box) Exports() modules.Exports {
	return modules.Exports{
		Default: box,
	}
}

type Config struct {
	ClientId       string `js:"clientId"`
	ClientSecret   string `js:"clientSecret"`
	BoxSubjectType string `js:"boxSubjectType"`
	BoxSubjectId   string `js:"boxSubjectId"`
}

var wg sync.WaitGroup

func (b *Box) ChunkedUpload(accessToken string, baseFolderId string, filePath string) {
	log.Println("Go - Found access token: ", accessToken)
	log.Println("Go - Found baseFolderId: ", baseFolderId)
	log.Println("Go - Found filePath: ", filePath)

	// cusr := CreateUploadSessionRequest{
	// 	FileName: fileName,
	// 	FileSize: fileSize,
	// 	FolderId: baseFolderId,
	// }

	// usr := CreateUploadSession(cusr, accessToken)
	// fmt.Printf("Found part size: %v and total parts: %v", usr.PartSize, usr.TotalParts)

	// uploadedParts := UploadParts(usr, *file, fileSize, accessToken)
	// sort.SliceStable(uploadedParts, func(i, j int) bool {
	// 	return uploadedParts[i].Offset < uploadedParts[j].Offset
	// })

	// fmt.Printf("Sorted parts: %+v", uploadedParts)

	// commitUploadBody := CommitUploadSessionRequest{
	// 	Parts: uploadedParts,
	// }

	// fileBytes, err := os.ReadFile(filePath)
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// h := sha1.New()
	// h.Write(fileBytes)
	// sha1_hash := base64.URLEncoding.EncodeToString(h.Sum(nil))
	// commitRes := CommitUploadSession(accessToken, sha1_hash, usr.SessionEndpoints.Commit, commitUploadBody)
	// log.Println("Found commit res: ", commitRes)
	// mi.upload = commitRes

}

func (b *Box) GetFileInfo(filePath string) (string, string, int) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println(err)
	}
	defer file.Close()
	log.Println("Go - Found file.Name(): ", file.Name())

	fileInfo, err := file.Stat()
	if err != nil {
		fmt.Println(err)
	}

	fileSize := int(fileInfo.Size())
	log.Println("Go - Found fileSize: ", fileSize)

	log.Println("Go - Found file name before format: ", fileInfo.Name())
	state := b.vu.State()

	fileName := fmt.Sprintf("%d.%d.%s", int(state.VUID), int(state.Iteration), fileInfo.Name())
	log.Println("Found file name: ", fileName)

	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Println(err)
	}
	h := sha1.New()
	h.Write(fileBytes)
	fileHash := base64.URLEncoding.EncodeToString(h.Sum(nil))

	return fileName, fileHash, fileSize
}

func (*Box) CreateUploadSession(fileName string, fileSize int, folderId string, accessToken string) UploadSessionResponse {
	log.Printf("Go - Found accessToken: %v", accessToken)

	cusr := CreateUploadSessionRequest{
		FileName: fileName,
		FileSize: fileSize,
		FolderId: folderId,
	}
	log.Printf("Go - Found cusr: %+v", cusr)

	apiUrl := "https://upload.box.com"
	resource := "/api/2.0/files/upload_sessions"

	u, _ := url.ParseRequestURI(apiUrl)
	u.Path = resource
	urlStr := u.String()

	bs, _ := json.Marshal(cusr)
	body := bytes.NewBuffer(bs)
	client := &http.Client{}
	r, _ := http.NewRequest(http.MethodPost, urlStr, body)
	var token = "Bearer " + accessToken
	r.Header.Set("Authorization", token)
	r.Header.Set("Content-Type", "application/json")

	res, _ := client.Do(r)
	log.Println("Found status: ", res.Status)
	resBody, error := io.ReadAll(res.Body)
	if error != nil {
		fmt.Println(error)
	}
	log.Println(string(resBody))

	var usr UploadSessionResponse
	json.Unmarshal(resBody, &usr)
	return usr
}

func (*Box) UploadParts(usr UploadSessionResponse, filePath string, fileSize int, accessToken string) []Part {
	totalParts := usr.TotalParts
	partSize := usr.PartSize
	chunksizes := make([]chunk, totalParts)
	processed := 0

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println(err)
	}
	defer file.Close()

	for i := 0; i < totalParts; i++ {
		byteDiff := fileSize - processed
		fmt.Printf("Found byte diff: %v, fileSize: %v, processBytes: %v", byteDiff, fileSize, processed)

		if byteDiff < partSize {
			fmt.Println("Found smaller diff: ", byteDiff)
			chunksizes[i].bufsize = byteDiff
		} else {
			chunksizes[i].bufsize = partSize
		}
		chunksizes[i].offset = int64(partSize * i)

		fmt.Printf("Found chunksizes: %v", chunksizes[i])
		fmt.Println("")
		processed += partSize
	}

	wg.Add(totalParts)
	uploadedParts := []Part{}
	for i := 0; i < totalParts; i++ {
		partchan := make(chan Part)

		chunk := chunksizes[i]
		buffer := make([]byte, chunk.bufsize)
		_, err := file.ReadAt(buffer, chunk.offset)

		if err != nil && err != io.EOF {
			fmt.Println(err)
		}

		h := sha1.New()
		h.Write(buffer)
		sha1_hash := base64.URLEncoding.EncodeToString(h.Sum(nil))

		go UploadSinglePart(usr, accessToken, sha1_hash, chunk, fileSize, buffer, partchan)
		part := <-partchan
		fmt.Println("Found part chan return: %+v", part)

		uploadedParts = append(uploadedParts, part)

	}
	wg.Wait()
	return uploadedParts
}

func UploadSinglePart(usr UploadSessionResponse, accessToken string, sha1_hash string, chunk chunk, fileSize int, buffer []byte, partchan chan<- Part) {
	body := bytes.NewBuffer(buffer)

	client := &http.Client{}
	r, _ := http.NewRequest(http.MethodPut, usr.SessionEndpoints.UploadPart, body)

	var token = "Bearer " + accessToken
	fmt.Println("Found token: ", token)

	digestHeader := fmt.Sprintf("sha=%s", sha1_hash)
	fmt.Print("Found digest header value: ", digestHeader)

	rangeHeader := fmt.Sprintf("bytes %d%s%d%s%d", chunk.offset, "-", int(chunk.offset)+chunk.bufsize-1, "/", fileSize)

	fmt.Println("Found range header value: ", rangeHeader)

	r.Header.Set("Authorization", token)
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("Digest", digestHeader)
	r.Header.Set("Content-Range", rangeHeader)

	res, _ := client.Do(r)
	fmt.Println("Found response status: ", res.Status)
	cl := res.ContentLength
	rbs := make([]byte, cl)
	res.Body.Read(rbs)
	fmt.Println("Found response body: ", string(rbs))

	var upr UploadedPartResponse
	json.Unmarshal(rbs, &upr)
	defer wg.Done()

	partchan <- upr.Part

}

func (*Box) CommitUploadSession(accessToken string, fileHash string, commitURL string, partsJson string) string {
	fmt.Println("")

	fmt.Println("Found commit upload payload: ", partsJson)
	fmt.Println("Found file hash: ", fileHash)
	fmt.Println("Found commit URL: ", commitURL)

	partsBytes := []byte(partsJson)
	body := bytes.NewBuffer(partsBytes)
	client := &http.Client{}
	r, _ := http.NewRequest(http.MethodPost, commitURL, body)
	var token = "Bearer " + accessToken
	r.Header.Set("Authorization", token)
	digestHeader := fmt.Sprintf("sha=%s", fileHash)
	r.Header.Set("Digest", digestHeader)
	r.Header.Set("Content-Type", "application/json")

	res, _ := client.Do(r)
	fmt.Println(res.Status)
	cl := res.ContentLength
	rbs := make([]byte, cl)
	res.Body.Read(rbs)
	fmt.Println(string(rbs))
	return string(rbs)
}

func (*Box) AccessToken(cfg Config) string {
	apiUrl := "https://api.box.com"
	resource := "/oauth2/token"
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", cfg.ClientId)
	data.Set("client_secret", cfg.ClientSecret)
	data.Set("box_subject_type", cfg.BoxSubjectType)
	data.Set("box_subject_id", cfg.BoxSubjectId)

	u, _ := url.ParseRequestURI(apiUrl)
	u.Path = resource
	urlStr := u.String()

	client := &http.Client{}
	r, _ := http.NewRequest(http.MethodPost, urlStr, strings.NewReader(data.Encode()))
	r.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, _ := client.Do(r)
	fmt.Println(resp.Status)

	cl := resp.ContentLength
	bs := make([]byte, cl)
	resp.Body.Read(bs)
	// fmt.Println(string(bs))

	var boxAuth BoxAuth
	json.Unmarshal(bs, &boxAuth)

	// rb.getAccessToken()

	// boxAuth.AccessToken
	return boxAuth.AccessToken
}

func (*Box) CreateBaseFolder(accessToken string, baseFolderId string) string {
	fmt.Println("Found accessToken")
	apiUrl := "https://api.box.com"
	resource := "/2.0/folders"

	now := time.Now().Format("2006-01-02T15:04:05")
	parentFolderName := fmt.Sprintf("H1-ChunkedUpload-%s", now)
	createFolderReq := CreateFolderRequest{
		Name: parentFolderName,
		Parent: Parent{
			Id: baseFolderId,
		},
	}

	u, _ := url.ParseRequestURI(apiUrl)
	u.Path = resource
	urlStr := u.String()

	bs, _ := json.Marshal(createFolderReq)
	body := bytes.NewBuffer(bs)
	client := &http.Client{}
	r, _ := http.NewRequest(http.MethodPost, urlStr, body)

	var token = "Bearer " + accessToken
	r.Header.Set("Authorization", token)
	r.Header.Set("Content-Type", "application/json")

	res, _ := client.Do(r)
	log.Println("Found status: ", res.Status)
	resBody, error := io.ReadAll(res.Body)
	if error != nil {
		fmt.Println(error)
	}
	log.Println(string(resBody))

	var folder FolderResponse
	json.Unmarshal(resBody, &folder)

	return folder.Id
}

type chunk struct {
	bufsize int
	offset  int64
}

type CommitUploadSessionRequest struct {
	Parts []Part `json:"parts"`
}

type UploadedPartResponse struct {
	Part Part `json:"part"`
}
type Part struct {
	Offset int    `json:"offset"`
	PartID string `json:"part_id"`
	Sha1   string `json:"sha1"`
	Size   int    `json:"size"`
}

type UploadSessionResponse struct {
	TotalParts        int              `json:"total_parts"`
	PartSize          int              `json:"part_size"`
	SessionEndpoints  SessionEndpoints `json:"session_endpoints"`
	SessionExpiresAt  time.Time        `json:"session_expires_at"`
	ID                string           `json:"id"`
	Type              string           `json:"type"`
	NumPartsProcessed int              `json:"num_parts_processed"`
}
type SessionEndpoints struct {
	ListParts  string `json:"list_parts"`
	Commit     string `json:"commit"`
	LogEvent   string `json:"log_event"`
	UploadPart string `json:"upload_part"`
	Status     string `json:"status"`
	Abort      string `json:"abort"`
}

type CreateUploadSessionRequest struct {
	FileName string `json:"file_name"`
	FileSize int    `json:"file_size"`
	FolderId string `json:"folder_id"`
}

type BoxAuth struct {
	AccessToken     string         `json:"access_token"`
	ExpiresIn       int            `json:"expires_in"`
	IssuedTokenType string         `json:"issued_token_type"`
	RefreshToken    string         `json:"refresh_token"`
	RestrictedTo    []RestrictedTo `json:"restricted_to"`
	TokenType       string         `json:"token_type"`
}
type FileVersion struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Sha1 string `json:"sha1"`
}
type Object struct {
	ID          string      `json:"id"`
	Etag        string      `json:"etag"`
	Type        string      `json:"type"`
	SequenceID  string      `json:"sequence_id"`
	Name        string      `json:"name"`
	Sha1        string      `json:"sha1"`
	FileVersion FileVersion `json:"file_version"`
}
type RestrictedTo struct {
	Scope  string `json:"scope"`
	Object Object `json:"object"`
}

type CreateFolderRequest struct {
	Name   string `json:"name"`
	Parent Parent `json:"parent"`
}
type Parent struct {
	Id string `json:"id"`
}

type FolderResponse struct {
	Id   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}
