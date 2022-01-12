package main

import (
	"context"
	"crwal/db"
	"crwal/model"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var domin = "https://www.domp4.cc/"
var wg sync.WaitGroup

func GetFetchUrl(url string) {
	res, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	res.Close = true
	res.Header.Add("Connection", "close")
	defer res.Body.Close()
	if res.StatusCode != 200 {
		log.Fatalf("status code error: %d %s", res.StatusCode, res.Status)
	}
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Fatal(err)
	}

	chans := make(chan model.Update, 200)

	fmt.Println("开始:", url, time.Now().Format("2006-01-02 15:04:05"))
	doc.Find("#vod .list-group-item").Each(func(i int, s *goquery.Selection) {
		hrefs, _ := s.Find("a").Attr("href")
		update := model.Update{}
		update.Type = "电影"
		update.CreatedTime = time.Now()
		update.Date = strings.TrimSpace(s.Find("b").Text())
		update.Title = strings.TrimSpace(s.Find("a").Text())
		update.Url = domin + strings.TrimSpace(hrefs)
		Regexp := regexp.MustCompile(`([^/]*?)\.html`)
		params := Regexp.FindStringSubmatch(update.Url)
		update.CId = params[1]

		if update.Url == "" {
			fmt.Println("不存在url", update)
			return
		}

		hasId := db.IsHasUpdateByUrl(update.Url)
		if hasId != "" {
			fmt.Println("已保存数据", update.Title)
			return
		}

		chans <- update
		fmt.Println("开始抓取", update.Url, update.Title, time.Now().Format("2006-01-02 15:04:05"))
	})

	doc.Find("#tv .list-group-item").Each(func(i int, s *goquery.Selection) {
		update := model.Update{}
		update.Type = "电视剧"
		update.CreatedTime = time.Now()
		hrefs, _ := s.Find("a").Attr("href")
		update.Date = strings.TrimSpace(s.Find("b").Text())
		update.Title = strings.TrimSpace(s.Find("a").Text())

		update.Url = domin + strings.TrimSpace(hrefs)
		Regexp := regexp.MustCompile(`([^/]*?)\.html`)
		params := Regexp.FindStringSubmatch(update.Url)
		update.CId = params[1]

		if update.Url == "" {
			fmt.Println("不存在url", update)
			return
		}

		hasId := db.IsHasUpdateByUrl(update.Url)
		if hasId != "" {
			fmt.Println("已保存数据", update.Title)
			return
		}

		chans <- update
		fmt.Println("开始抓取", update.Url, update.Title, time.Now().Format("2006-01-02 15:04:05"))
	})

	close(chans)

	wg.Add(5)
	for i := 1; i <= 5; i++ {
		go CrwalInfo(chans, &wg)
	}
	wg.Wait()
}

func CrwalInfo(chans chan model.Update, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case update, ok := <-chans:
			if ok {
				info := db.GetDyInfo(update.Url)
				if info.LongTitle != update.Title {
					dy := model.Dy{}
					dy.Url = update.Url
					dy.LongTitle = update.Title
					dy.CId = update.CId
					dy_info := GetContentNewAll(&dy)
					info_id := db.SaveDy(&dy_info)

					down_info := model.DownInfoStruct{}
					down_info.DownUrl = dy.DownUrl
					down_info.Url = dy.Url
					down_info.Title = dy.Title
					down_info.LongTitle = dy.LongTitle
					down_info.DownStatus = dy.DownStatus
					down_info.CId = dy.CId
					down_info.Type = dy.Type
					down_info.UpdatedTime = dy.UpdatedTime
					down_info.CreatedTime = dy.CreatedTime
					db.SaveAndUpdateDownInfo(&down_info)

					update.InfoId = info_id
					db.SaveUpdate(&update)
				} else {
					update.InfoId = info.ID.Hex()
					db.SaveUpdate(&update)
				}

			} else {
				fmt.Println("退出", time.Now().Format("2006-01-02 15:04:05"))
				goto forEnd
			}

		}
	}
forEnd:
	return
}

//获取网站上爬取的数据
func GetHttpHtmlContent(url string, selector string, sel interface{}) (string, error) {
	options := []chromedp.ExecAllocatorOption{
		chromedp.Flag("headless", true), // debug使用
		chromedp.Flag("blink-settings", "imagesEnabled=false"),
		chromedp.UserAgent(`Mozilla/5.0 (Windows NT 6.3; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/73.0.3683.103 Safari/537.36`),
	}
	//初始化参数，先传一个空的数据
	options = append(chromedp.DefaultExecAllocatorOptions[:], options...)

	c, _ := chromedp.NewExecAllocator(context.Background(), options...)

	// create context
	chromeCtx, cancels := chromedp.NewContext(c, chromedp.WithLogf(log.Printf))
	defer cancels()
	// 执行一个空task, 用提前创建Chrome实例
	chromedp.Run(chromeCtx, make([]chromedp.Action, 0, 1)...)

	//创建一个上下文，超时时间为40s
	timeoutCtx, cancel := context.WithTimeout(chromeCtx, 2*time.Minute)
	defer cancel()

	var htmlContent string
	err := chromedp.Run(timeoutCtx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(selector),
		chromedp.OuterHTML(sel, &htmlContent, chromedp.ByJSPath),
	)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	//log.Println(htmlContent)

	return htmlContent, nil
}

func GetContentNewAll(dy *model.Dy) model.Dy {
	htmlContent, _ := GetHttpHtmlContent(dy.Url, "#download1", "document.querySelector(\"body\")")
	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Printf(dy.Url)
	dy.UpdatedTime = time.Now()
	dy.CreatedTime = time.Now()
	dy.Type = []string{strings.TrimSpace(doc.Find(".post-meta span").Eq(0).Find("a").Text())}
	dy.ProductionDate = strings.TrimSpace(doc.Find(".pubtime").Text())
	dy.Pic, _ = doc.Find(".pic img").Attr("src")
	dy.Title = strings.TrimSpace(doc.Find(".text p").Eq(0).Find("span").Text())
	dy.LongTitle = strings.TrimSpace(doc.Find(".article-header h1").Text())
	dy.Type = []string{strings.TrimSpace(doc.Find(".breadcrumb").Find("li").Eq(1).Text())}
	if dy.Type[0] == "电视剧" {
		match, _ := regexp.MatchString(`全\d*集$`, dy.LongTitle)
		if match {
			dy.DownStatus = 1
		} else {
			match2, _ := regexp.MatchString(`更新至\d*集$`, dy.LongTitle)
			if match2 {
				dy.DownStatus = 0
			}
		}
	}

	em := doc.Find(".text p").Eq(1).Find("em").Text()
	if em == "别名：" {
		alias := strings.TrimSpace(doc.Find(".text p").Eq(1).Find("span").Text())
		re_alias := strings.Split(alias, "/")
		for k, v := range re_alias {
			re_alias[k] = strings.TrimSpace(v)
		}
		dy.Alias = re_alias
	}

	dy.Rating = strings.TrimSpace(doc.Find(".rating_num ").Text())
	dy.DoubanUrl, _ = doc.Find(".rating_num a").Attr("href")
	dy.DoubanId, _ = doc.Find(".rating_num").Attr("subject")

	star := []string{}
	doc.Find(".actor .attrs span").Each(func(i int, s *goquery.Selection) {
		star = append(star, strings.TrimSpace(s.Find("a").Text()))
	})
	dy.Stars = star

	dirct := []string{}
	doc.Find(".director .attrs span").Each(func(i int, s *goquery.Selection) {
		dirct = append(dirct, strings.TrimSpace(s.Find("a").Text()))
	})
	dy.Director = dirct

	dy.Area = strings.TrimSpace(doc.Find(".director").Next().Find("span").Text())
	dy.Year = strings.TrimSpace(doc.Find(".director").Next().Next().Find("span").Text())
	dy.Language = strings.TrimSpace(doc.Find(".director").Next().Next().Next().Find("span").Text())
	dy.RunTime = strings.TrimSpace(doc.Find(".director").Next().Next().Next().Next().Find("span").Text())

	tags := []string{}
	doc.Find(".text .tag a").Each(func(i int, s *goquery.Selection) {
		tags = append(tags, strings.TrimSpace(s.Text()))
	})
	dy.Tags = tags

	dy.Introduction = strings.TrimSpace(doc.Find(".article-related").Find("p").Eq(0).Text())

	down_Urls := []model.DownStruct{}
	doc.Find(".url-left").Each(func(i int, s *goquery.Selection) {
		t, _ := s.Find(".url-left a").Attr("title")
		h, _ := s.Find(".url-left a").Attr("href")
		reg, _ := regexp.Compile(`[^:]+`)
		down_Urls = append(down_Urls, model.DownStruct{t, h, reg.FindString(h)})
	})

	dy.DownUrl = down_Urls
	return *dy
}

func main() {
	fmt.Println("抓取开始", time.Now())
	starts := time.Now().Unix()
	url := "https://www.domp4.cc/custom/update.html"
	GetFetchUrl(url)
	ends := time.Now().Unix()

	fmt.Println("抓取结束", time.Now())
	fmt.Println("耗时", time.Now(), starts, ends, ends-starts)
}
