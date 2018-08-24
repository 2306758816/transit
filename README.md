# transit

![transit](https://raw.githubusercontent.com/wenjiax/transit/master/transit.jpg)

### 文件介绍

```
transit/
├── README.md
├── http-server
│   ├── main.go     HTTP服务
│   └── upload      上传文件存放目录
│       └── 614.png
├── transit-client
│   └── main.go     处理服务器转发的数据并请求HTTP服务
├── transit-server
│   ├── config.json         转发配置存放文件
│   ├── html                静态资源
│   │   ├── config.html
│   │   ├── css
│   │   ├── example.html
│   │   ├── fonts
│   │   ├── index.html
│   │   └── js
│   └── main.go             转发服务器及配置管理
└── transit.go              核心文件,处理转发
```

### 启动顺序

1. 启动 transit-server 提供 transit 配置及转发服务。
2. 启动 transit-client 接收转发并请求 HTTP 服务。
3. 启动 http-server 后端 HTTP API 服务。

#### 访问 http://127.0.0.1:9090/transit/example.html
