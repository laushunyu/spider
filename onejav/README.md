# onejav

## Require
You should set GOPROXY sometimes.

```
export GOPROXY=https://proxy.golang.com.cn,direct 
```

## Install
Build by yourself:

```
go install github.com/laushunyu/spider/onejav@latest
```

## Guide
Download by time:
```
# today
onejav -h <xxx>.com time now
# specify a date
onejav -h <xxx>.com time 2022-3-10
```

Download by popular:
```
# last 7 days top 50
onejav -h <xxx>.com popular 7 50 
```

Download by url:
```
# all aritifact from this page to last page
onejav -h <xxx>.com https://xxx.com/actress/Kana%20Momonogi 
```

You can also set `-p <num>` to use <num> goroutine to download concurrently 

## Q&A
If you cannot access host, proxy is necessary.

```
export HTTP_PROXY=127.0.0.1:7890
export HTTPS_PROXY=127.0.0.1:7890
```

