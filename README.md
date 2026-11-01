# 阿里云盘Token获取（普通 / TV）

基于lilu0826/alitv_openlist维护。Railway、Docker及本地Go服务现在提供两种扫码入口：

| 入口 | 用途 | 小雅配置 |
|---|---|---|
| TV Token（默认首页） | 获取TV刷新令牌，原有刷新接口保持`/token` | `ALIYUN_OPEN_TOKEN` / `myopentoken.txt` |
| 普通短token（首页`#basic`） | 获取普通阿里云盘refresh_token，通常为32位，以实际签发为准 | `ALIYUN_TOKEN` / `mytoken.txt` |

打开页面选择类型，用阿里云盘App扫码并确认，再点击复制令牌。两种令牌不能互换，也不要截断或修改返回值。二维码过期或取消后，可点击重新生成。普通扫码不使用`/token`这个TV刷新接口。

## Railway部署

从本仓库的`main`分支构建，使用根目录Dockerfile。程序读取Railway提供的`PORT`，未设置时监听8081。普通扫码无需增加环境变量或持久卷；服务必须能通过HTTPS访问`passport.aliyundrive.com`。

普通二维码由服务本身生成，扫码会话及上游Cookie仅临时保存在内存，最多保留5分钟，成功或取消后清除。刷新令牌只返回当前页面，不保存到数据库、浏览器存储或服务器日志。重启或重新部署后，正在使用的二维码需要重新生成。

普通扫码使用`POST /basic/qr`生成会话，再以JSON `{"session_id":"生成接口返回的值"}`调用`POST /basic/check`。检查结果可能为`NEW`、`SCANED`、`EXPIRED`、`CANCELED`或`CONFIRMED`；成功时仅提供普通`refresh_token`及`token_length`，不暴露完整登录资料。

本次普通扫码功能用于根目录Go服务（Railway/Docker）；`api/`中的旧Vercel入口仍保留原TV功能。上游镜像不包含本仓库的新功能，部署时请使用本仓库源码。

## 验证

```sh
go test ./...
go vet ./...
go build .
```

自动测试覆盖合成扫码成功结果、Cookie隔离、终态清理、容量限制及错误处理；真实账号扫码是否成功仍以用户实际验证为准。

以下保留原项目的TV使用说明。

# alitv_openlist

通过阿里云盘TV获取token,只需要超级会员即可不限速、不限流量使用（无需购买第三方流量包）

# 源码运行
需要go语言环境
- 克隆代码
- go mod download
- go run main.go
- 访问http://localhost:8081/

# 下载编译产物直接运行
在[Releases](https://github.com/lilu0826/alitv_openlist/releases)页面下载即可

运行后访问http://localhost:8081/

# docker部署
```
docker run --name=alitv_openlist -d -p 8081:8081 --restart=always ghcr.io/lilu0826/alitv_openlist:latest
```

# 使用教程
访问 http://IP:PORT/

![alt text](image.png)

用阿里云盘扫码登录，输入框会出现refresh_ token,复制refresh_token



![alt text](image-2.png)

将refresh_token填写到OpenList的刷新令牌

再把使用在线API勾上

API地址填写：http://IP:PORT/token

确保OpenList部署的机器能访问到http://IP:PORT/token

完事

# Alist兼容

将扫码得到的刷新令牌和Oauth令牌链接填入即可

![alt text](image-1.png)
