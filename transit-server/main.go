package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/wenjiax/transit"
)

// 转发规则临时保存
var rule map[string]string

var writeCh, reloadCh, restartCh chan struct{}

func init() {
	writeCh = make(chan struct{}, 1)
	reloadCh = make(chan struct{}, 1)
	restartCh = make(chan struct{}, 1)

	rule = make(map[string]string)
	_, err := os.Stat("./config.json")
	if err == nil || os.IsExist(err) {
		b, err := ioutil.ReadFile("./config.json")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = json.Unmarshal(b, &rule)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
	go saveConfig()
}

func server(count *int, rule map[string]string) error {
	copyRule := make(map[string]string)
	// rule["127.0.0.1"] = "10.17.82.64:8001"
	// rule["127.0.0.1"] = "127.0.0.1:8001"
	// 深拷贝map
	b, _ := json.Marshal(rule)
	json.Unmarshal(b, &copyRule)

	t := transit.NewTransit(copyRule)

	go func() {
		<-restartCh
		fmt.Println("restart........")
		t.Close()
	}()

	log.Println("listen tcp serve on :8000.....")
	err := t.ListenAndServe(":8000")
	if err != nil {
		fmt.Println("err...........", err)
		if *count > 0 {
			*count++
		} else {
			return err
		}
	}
	return nil
}

func main() {
	errch := make(chan error, 1)

	// 启动TCP服务
	go func() {
		var count int
	Transit:
		if err := server(&count, rule); err != nil {
			errch <- err
		}
		if count >= 5 {
			fmt.Println(count, "次尝试重启,未成功")
			os.Exit(1)
		}
		count++
		fmt.Println("开始", count, "次尝试重启")
		time.Sleep(1 * time.Second)
		goto Transit
	}()

	// 启动HTTP服务
	go func() {
		targetURL, err := url.Parse("http://127.0.0.1:8000")
		if err != nil {
			errch <- err
			return
		}
		http.Handle("/transit/", http.StripPrefix("/transit/", http.FileServer(http.Dir("./html"))))

		proxy := httputil.NewSingleHostReverseProxy(targetURL)

		http.Handle("/", proxy)

		http.HandleFunc("/getConfig", getConfig)

		http.HandleFunc("/update", update)

		http.HandleFunc("/add", add)

		http.HandleFunc("/addTo", addTo)

		log.Println("listen http proxy on :9090.....")
		err = http.ListenAndServe(":9090", nil)
		if err != nil {
			errch <- err
		}
	}()
	err := <-errch
	fmt.Println(err)
}

// saveConfig 保存配置信息
func saveConfig() {
	for {
		<-writeCh
		b, err := json.Marshal(rule)
		if err != nil {
			fmt.Println(err)
			continue
		}
		f, error := os.OpenFile("./tmp.txt", os.O_RDWR|os.O_CREATE, 0766)
		if error != nil {
			fmt.Println(error)
			continue
		}
		_, err = f.Write(b)
		if err != nil {
			fmt.Println(err)
			f.Close()
			continue
		}
		f.Close()
		_, err = os.Stat("./config.json")
		if err == nil || os.IsExist(err) {
			os.Remove("./config.json")
		}
		os.Rename("./tmp.txt", "./config.json")
		// reloadCh <- struct{}{}
		fmt.Println("send restart..........")
		restartCh <- struct{}{}
	}
}

// addTo 添加至体验
func addTo(w http.ResponseWriter, r *http.Request) {
	ip := r.PostFormValue("ip")
	to := r.PostFormValue("to")
	ip = strings.TrimSpace(ip)
	to = strings.TrimSpace(to)
	fmt.Println(ip, to)
	var key string
	for k, v := range rule {
		if strings.Contains(k, ip) {
			fmt.Fprintln(w, "当前IP已经存在配置!")
			return
		}
		if v == to {
			key = k
		}
	}
	if key == "" {
		fmt.Fprintln(w, "体验目标IP错误!")
		return
	}
	delete(rule, key)
	nkey := key + ";" + ip
	fmt.Println(nkey)
	rule[nkey] = to
	// keyMap[key] = nkey
	writeCh <- struct{}{}
	fmt.Fprintln(w, "修改配置成功!")
}

// add 添加 Transit 配置
func add(w http.ResponseWriter, r *http.Request) {
	data := r.PostFormValue("data")
	kv := strings.Split(data, "::")
	if len(kv) == 2 {
		for k, v := range rule {
			if strings.Contains(k, kv[0]) {
				fmt.Fprintln(w, "请勿添加重复的被转发IP!")
				return
			}
			if strings.Contains(v, kv[1]) {
				fmt.Fprintln(w, "请勿添加重复的目标IP!")
				return
			}
		}
		rule[kv[0]] = kv[1]
		fmt.Fprintln(w, "添加配置成功!")
		writeCh <- struct{}{}
	} else {
		fmt.Fprintln(w, "请检查添加的信息是否正确!")
	}
}

// update 更新 Transit 配置
func update(w http.ResponseWriter, r *http.Request) {
	oldKey := r.PostFormValue("oldKey")
	data := r.PostFormValue("data")
	kv := strings.Split(data, "::")
	if len(kv) == 2 {
		for k, v := range rule {
			if k == oldKey {
				continue
			}
			if strings.Contains(k, kv[0]) {
				fmt.Fprintln(w, "请勿添加重复的被转发IP!")
				return
			}
			if strings.Contains(v, kv[1]) {
				fmt.Fprintln(w, "请勿添加重复的目标IP!")
				return
			}
		}
		if kv[0] != oldKey || kv[1] != rule[oldKey] {
			delete(rule, oldKey)
			rule[kv[0]] = kv[1]
			// keyMap[oldKey] = kv[0]
			writeCh <- struct{}{}
		}
		fmt.Fprintln(w, "修改配置成功!")
	} else {
		fmt.Fprintln(w, "请检查修改的信息是否正确!")
	}
}

// getConfig 获取 Transit 配置信息
func getConfig(w http.ResponseWriter, r *http.Request) {
	b, _ := json.Marshal(rule)
	fmt.Fprintln(w, string(b))
}
