// Command zentaoctl 是禅道集成的实机校验工具：登录真实禅道，抓取指派
// 列表与操作表单页存为夹具，并用本仓库的解析器即时验证。网络可达时跑
// 一次，即可确认「需求列表」「事项操作」的页面结构假设是否成立。
//
// 用法（在仓库根目录）：
//
//	go run ./cmd/zentaoctl -base http://192.168.2.225/biz/ \
//		-account 账号 -password 密码
//
// 可选：
//
//	-form task:start:108   抓取一条任务的操作表单页（默认只抓取，不提交）
//	-submit                配合 -form 真正提交操作（会写禅道，谨慎使用）
//	-out testdata/live     夹具输出目录（默认 testdata/live）
//
// 解析失败时以非零码退出，并在输出里指明哪一页结构不符合假设。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chandao/zentaotray/internal/domain"
	"chandao/zentaotray/internal/infrastructure/zentao"
)

func main() {
	base := flag.String("base", "http://192.168.2.225/biz/", "禅道基础地址")
	account := flag.String("account", "", "禅道账号")
	password := flag.String("password", "", "禅道密码")
	out := flag.String("out", filepath.Join("testdata", "live"), "夹具输出目录")
	formSpec := flag.String("form", "", "抓取操作表单页，格式 objectType:action:id，如 task:start:108")
	submit := flag.Bool("submit", false, "配合 -form 真正提交操作（默认只抓表单不提交）")
	flag.Parse()

	if *account == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "需要 -account 与 -password")
		os.Exit(2)
	}
	failures := 0
	fail := func(format string, args ...any) {
		failures++
		fmt.Printf("  ✗ "+format+"\n", args...)
	}
	ok := func(format string, args ...any) {
		fmt.Printf("  ✓ "+format+"\n", args...)
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建输出目录: %v\n", err)
		os.Exit(2)
	}
	client, err := zentao.New(*base, *account, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "构造客户端: %v\n", err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fmt.Println("登录…")
	if err := client.Login(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "登录失败（检查地址/账号/网络）: %v\n", err)
		os.Exit(1)
	}
	ok("登录成功")

	// 指派列表三件套：保存原始页并用真实解析器验证。
	type listCase struct {
		name  string
		fetch func(context.Context) ([]byte, error)
		parse func([]byte, string) ([]domain.Item, error)
		kind  domain.ItemType
	}
	baseURL := strings.TrimRight(*base, "/") + "/"
	lists := []listCase{
		{"my-task", func(ctx context.Context) ([]byte, error) { return client.FetchAssignedTasks(ctx) }, zentao.ParseAssignedTasks, domain.ItemTask},
		{"my-bug", func(ctx context.Context) ([]byte, error) { return client.FetchAssignedBugs(ctx) }, zentao.ParseAssignedBugs, domain.ItemBug},
		{"my-story", func(ctx context.Context) ([]byte, error) { return client.FetchAssignedStories(ctx) }, zentao.ParseAssignedStories, domain.ItemStory},
	}
	for _, lc := range lists {
		fmt.Printf("抓取 %s…\n", lc.name)
		body, err := lc.fetch(ctx)
		if err != nil {
			fail("%s 抓取失败: %v", lc.name, err)
			continue
		}
		path := filepath.Join(*out, lc.name+".html")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			fail("%s 写夹具失败: %v", lc.name, err)
			continue
		}
		items, err := lc.parse(body, baseURL+"index.php?m=my&f="+string(lc.kind)+"&browseType=assignedTo")
		paging := errors.Is(err, zentao.ErrPaginationRequired)
		if err != nil && !paging {
			fail("%s 解析失败——页面结构与解析器假设不一致，把 %s 发给维护者调整解析器: %v", lc.name, path, err)
			continue
		}
		summary := "空"
		if len(items) > 0 {
			summary = fmt.Sprintf("首条 %s", items[0].Key())
			if lc.kind == domain.ItemStory && items[0].Project == "" {
				fail("%s 首条需求项目名为空（计划/产品列名与假设不符）", lc.name)
				continue
			}
		}
		note := ""
		if paging {
			note = "（首页解析成功；列表还有后续页，本工具只校验首页 ≤100 条）"
		}
		ok("%s 解析出 %d 条（%s），夹具 %s%s", lc.name, len(items), summary, path, note)
	}

	// 操作表单页：默认只抓取并解析出字段，不提交。
	if *formSpec != "" {
		parts := strings.Split(*formSpec, ":")
		if len(parts) != 3 {
			fmt.Fprintln(os.Stderr, "-form 格式应为 objectType:action:id")
			os.Exit(2)
		}
		objectType, action, id := parts[0], parts[1], parts[2]
		fmt.Printf("抓取操作表单 %s…\n", *formSpec)
		form, err := client.FetchActionForm(ctx, objectType, action, id)
		if err != nil {
			fail("操作表单抓取/解析失败——操作页结构与 actions.go 假设不一致: %v", err)
		} else {
			path := filepath.Join(*out, fmt.Sprintf("action-%s-%s-%s.html", objectType, action, id))
			fmt.Printf("  ✓ 表单 %s，字段 %d 个:", form.Path, len(form.Fields))
			for key, value := range form.Fields {
				fmt.Printf(" %s=%q", key, value)
			}
			fmt.Printf("（页面已存 %s）\n", path)
			if *submit {
				if _, err := client.SubmitAction(ctx, form, nil); err != nil {
					fail("提交失败: %v", err)
				} else {
					ok("已提交 %s %s %s（去禅道页面核对结果）", objectType, id, action)
				}
			} else {
				fmt.Println("  · 未提交（加 -submit 才会真正写入禅道）")
			}
		}
	}

	if failures > 0 {
		fmt.Printf("\n%d 项不通过：把 testdata/live/ 下的页面与输出发给维护者即可修复解析器。\n", failures)
		os.Exit(1)
	}
	fmt.Println("\n全部通过：列表与操作页的结构假设成立。")
}
