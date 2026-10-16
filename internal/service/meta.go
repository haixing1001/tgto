package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"tgto/internal/logger"
)

// MediaItem 影视条目。
type MediaItem struct {
	ID             string   `json:"id"`
	Source         string   `json:"source"` // tmdb / douban / anilist / bangumi / actor
	Title          string   `json:"title"`
	OriginalTitle  string   `json:"original_title"`
	Year           int      `json:"year"`
	MediaType      string   `json:"media_type"` // movie / tv
	Genres         []string `json:"genres"`
	Region         string   `json:"region"`
	Rating         float64  `json:"rating"`
	Votes          int      `json:"votes"`
	Overview       string   `json:"overview"`
	TMDBID         string   `json:"tmdb_id"`
	QualityTag     string   `json:"quality_tag"`
	PosterColor    string   `json:"poster_color"`
	Platforms      []string `json:"platforms"`
	AiringDate     string   `json:"airing_date,omitempty"`
	EpisodeLabel   string   `json:"episode_label,omitempty"`
}

// DiscoveryQuery 影视探索查询参数。
type DiscoveryQuery struct {
	Source   string
	Keyword  string
	Category string // 全部 / 电影 / 剧集
	Genre    string
	Region   string
	Year     string
	Sort     string
	Page     int
}

// sampleCatalog 内置示例片库（未配置 TMDB Key 时的演示数据，字段与 TMDB 响应同构）。
var sampleCatalog = []MediaItem{
	{ID: "s01", Source: "tmdb", Title: "幕府将军", OriginalTitle: "Shogun", Year: 2024, MediaType: "tv", Genres: []string{"剧情", "历史", "战争"}, Region: "美国", Rating: 8.7, Votes: 1240, Overview: "1600年的日本，英国水手约翰·布莱克索恩卷入大名之间的权力斗争。", TMDBID: "126308", QualityTag: "4K REMUX", PosterColor: "#7c3aed", Platforms: []string{"Hulu"}},
	{ID: "s02", Source: "tmdb", Title: "沙丘2", OriginalTitle: "Dune: Part Two", Year: 2024, MediaType: "movie", Genres: []string{"科幻", "冒险"}, Region: "美国", Rating: 8.5, Votes: 3102, Overview: "保罗·厄崔迪与契妮和弗雷曼人结盟，走上复仇之路。", TMDBID: "693134", QualityTag: "2160p", PosterColor: "#b45309", Platforms: []string{"HBO Max"}},
	{ID: "s03", Source: "tmdb", Title: "繁花", OriginalTitle: "Blossoms Shanghai", Year: 2023, MediaType: "tv", Genres: []string{"剧情", "爱情"}, Region: "中国大陆", Rating: 8.4, Votes: 890, Overview: "九十年代的上海，阿宝在时代浪潮中的沉浮。", TMDBID: "225372", QualityTag: "1080p", PosterColor: "#be185d", Platforms: []string{"腾讯"}},
	{ID: "s04", Source: "douban", Title: "漫长的季节", OriginalTitle: "The Long Season", Year: 2023, MediaType: "tv", Genres: []string{"悬疑", "家庭"}, Region: "中国大陆", Rating: 9.4, Votes: 560, Overview: "东北小城，一桩碎尸案跨越近二十年。", TMDBID: "225371", QualityTag: "2160p", PosterColor: "#0f766e", Platforms: []string{"腾讯"}},
	{ID: "s05", Source: "anilist", Title: "咒术回战 第二季", OriginalTitle: "Jujutsu Kaisen 2nd Season", Year: 2023, MediaType: "tv", Genres: []string{"动作", "动画"}, Region: "日本", Rating: 8.6, Votes: 2210, Overview: "五条悟的过去被揭开， followed by 涉谷事变。", TMDBID: "114410", QualityTag: "1080p", PosterColor: "#4338ca", Platforms: []string{"Crunchyroll"}},
	{ID: "s06", Source: "bangumi", Title: "葬送的芙莉莲", OriginalTitle: "Sousou no Frieren", Year: 2023, MediaType: "tv", Genres: []string{"奇幻", "动画"}, Region: "日本", Rating: 9.2, Votes: 1680, Overview: "勇者一行人结束冒险后，精灵魔法使芙莉莲踏上新的旅程。", TMDBID: "209867", QualityTag: "1080p", PosterColor: "#0369a1", Platforms: []string{"Crunchyroll"}},
	{ID: "s07", Source: "tmdb", Title: "奥本海默", OriginalTitle: "Oppenheimer", Year: 2023, MediaType: "movie", Genres: []string{"传记", "历史"}, Region: "美国", Rating: 8.3, Votes: 4200, Overview: "原子弹之父奥本海默的传记史诗。", TMDBID: "872585", QualityTag: "2160p", PosterColor: "#1e293b", Platforms: []string{"Peacock"}},
	{ID: "s08", Source: "tmdb", Title: "三体", OriginalTitle: "Three-Body", Year: 2023, MediaType: "tv", Genres: []string{"科幻", "悬疑"}, Region: "中国大陆", Rating: 8.7, Votes: 990, Overview: "物理学家们接连自杀，警官史强与纳米学者汪淼揭开真相。", TMDBID: "108676", QualityTag: "2160p", PosterColor: "#065f46", Platforms: []string{"腾讯"}},
	{ID: "s09", Source: "douban", Title: "周处除三害", OriginalTitle: "The Pig, the Snake and the Pigeon", Year: 2023, MediaType: "movie", Genres: []string{"动作", "犯罪"}, Region: "中国台湾", Rating: 8.2, Votes: 640, Overview: "通缉犯陈桂林在生命尽头决定除掉排在通缉令前的两大恶人。", TMDBID: "1094167", QualityTag: "1080p", PosterColor: "#9a3412", Platforms: []string{}},
	{ID: "s10", Source: "netflix", Title: "鱿鱼游戏 第二季", OriginalTitle: "Squid Game S2", Year: 2024, MediaType: "tv", Genres: []string{"惊悚", "剧情"}, Region: "韩国", Rating: 8.0, Votes: 1800, Overview: "成奇勋再次回到游戏中，誓要终结这场杀戮。", TMDBID: "93405", QualityTag: "4K HDR", PosterColor: "#db2777", Platforms: []string{"Netflix"}},
	{ID: "s11", Source: "netflix", Title: "三体：Netflix版", OriginalTitle: "3 Body Problem", Year: 2024, MediaType: "tv", Genres: []string{"科幻", "剧情"}, Region: "美国", Rating: 7.6, Votes: 1200, Overview: "科学家的集体死亡将五位昔日同窗卷入人类史上最大的阴谋。", TMDBID: "108519", QualityTag: "4K HDR", PosterColor: "#1d4ed8", Platforms: []string{"Netflix"}},
	{ID: "s12", Source: "hbo", Title: "龙之家族 第二季", OriginalTitle: "House of the Dragon S2", Year: 2024, MediaType: "tv", Genres: []string{"奇幻", "剧情"}, Region: "美国", Rating: 8.4, Votes: 1500, Overview: "坦格利安家族的内战升级为巨龙之战。", TMDBID: "94997", QualityTag: "4K HDR", PosterColor: "#7f1d1d", Platforms: []string{"HBO"}},
	{ID: "s13", Source: "apple", Title: "人生切割术 第二季", OriginalTitle: "Severance S2", Year: 2025, MediaType: "tv", Genres: []string{"科幻", "惊悚"}, Region: "美国", Rating: 8.8, Votes: 720, Overview: "宏观数据精算部的员工们发现自己记忆被手术切割。", TMDBID: "95394", QualityTag: "4K HDR", PosterColor: "#0e7490", Platforms: []string{"Apple TV+"}},
	{ID: "s14", Source: "disney", Title: "洛基 第二季", OriginalTitle: "Loki S2", Year: 2023, MediaType: "tv", Genres: []string{"科幻", "冒险"}, Region: "美国", Rating: 8.2, Votes: 1350, Overview: "洛基在时间变异管理局崩溃后寻找自己的位置。", TMDBID: "84958", QualityTag: "4K HDR", PosterColor: "#166534", Platforms: []string{"Disney+"}},
	{ID: "s15", Source: "amazon", Title: "辐射", OriginalTitle: "Fallout", Year: 2024, MediaType: "tv", Genres: []string{"科幻", "冒险"}, Region: "美国", Rating: 8.5, Votes: 1100, Overview: "核战两百年后，避难所居民踏上地面废土。", TMDBID: "106379", QualityTag: "4K HDR", PosterColor: "#a16207", Platforms: []string{"Amazon Prime"}},
	{ID: "s16", Source: "hulu", Title: "幕府将军", OriginalTitle: "Shogun", Year: 2024, MediaType: "tv", Genres: []string{"剧情", "历史"}, Region: "美国", Rating: 8.7, Votes: 1240, Overview: "1600年的日本，英国水手约翰·布莱克索恩卷入大名之间的权力斗争。", TMDBID: "126308", QualityTag: "4K REMUX", PosterColor: "#7c3aed", Platforms: []string{"Hulu"}},
	{ID: "s17", Source: "anilist", Title: "我推的孩子", OriginalTitle: "Oshi no Ko", Year: 2023, MediaType: "tv", Genres: []string{"剧情", "动画"}, Region: "日本", Rating: 8.5, Votes: 1430, Overview: "转生为偶像孩子的双胞胎追寻母亲死亡的真相。", TMDBID: "203737", QualityTag: "1080p", PosterColor: "#c026d3", Platforms: []string{"Crunchyroll"}},
	{ID: "s18", Source: "douban", Title: "流浪地球2", OriginalTitle: "The Wandering Earth II", Year: 2023, MediaType: "movie", Genres: []string{"科幻", "灾难"}, Region: "中国大陆", Rating: 8.3, Votes: 3400, Overview: "太阳危机前夕，人类启动流浪地球计划。", TMDBID: "946310", QualityTag: "2160p", PosterColor: "#1e40af", Platforms: []string{"腾讯"}},
	{ID: "s19", Source: "tmdb", Title: "因果报应", OriginalTitle: "Maharaja", Year: 2024, MediaType: "movie", Genres: []string{"悬疑", "动作"}, Region: "印度", Rating: 8.3, Votes: 560, Overview: "一个普通男人的“垃圾桶”引发的惊天复仇。", TMDBID: "1189768", QualityTag: "1080p", PosterColor: "#92400e", Platforms: []string{"Netflix"}},
	{ID: "s20", Source: "bangumi", Title: "药屋少女的呢喃", OriginalTitle: "The Apothecary Diaries", Year: 2023, MediaType: "tv", Genres: []string{"悬疑", "动画"}, Region: "日本", Rating: 8.7, Votes: 980, Overview: "后宫中药师少女猫猫破解一桩桩宫廷谜案。", TMDBID: "206887", QualityTag: "1080p", PosterColor: "#4d7c0f", Platforms: []string{"Crunchyroll"}},
	{ID: "s21", Source: "actor", Title: "巩俐", OriginalTitle: "Gong Li", Year: 0, MediaType: "person", Genres: []string{"演员"}, Region: "中国大陆", Rating: 9.1, Votes: 5200, Overview: "代表作：《霸王别姬》《红高粱》《活着》。", QualityTag: "", PosterColor: "#881337"},
	{ID: "s22", Source: "actor", Title: "汤姆·哈迪", OriginalTitle: "Tom Hardy", Year: 0, MediaType: "person", Genres: []string{"演员"}, Region: "英国", Rating: 8.9, Votes: 3800, Overview: "代表作：《盗梦空间》《敦刻尔克》《毒液》。", QualityTag: "", PosterColor: "#3730a3"},
	{ID: "s23", Source: "actor", Title: "陈道明", OriginalTitle: "Chen Daoming", Year: 0, MediaType: "person", Genres: []string{"演员"}, Region: "中国大陆", Rating: 9.0, Votes: 4300, Overview: "代表作：《康熙王朝》《归来》《我的前半生》。", QualityTag: "", PosterColor: "#155e75"},
	{ID: "s24", Source: "actor", Title: "安藤樱", OriginalTitle: "Sakura Ando", Year: 0, MediaType: "person", Genres: []string{"演员"}, Region: "日本", Rating: 8.8, Votes: 2100, Overview: "代表作：《小偷家族》《重启人生》。", QualityTag: "", PosterColor: "#6b21a8"},
}

// trendingSources 榜单页平台列表。
var trendingSources = []struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	ItemKey string `json:"item_key"`
}{
	{Key: "hot", Name: "热门", Source: "tmdb", ItemKey: "s02"},
	{Key: "netflix", Name: "Netflix", Source: "netflix", ItemKey: "s10"},
	{Key: "hbo", Name: "HBO", Source: "hbo", ItemKey: "s12"},
	{Key: "apple", Name: "Apple TV+", Source: "apple", ItemKey: "s13"},
	{Key: "disney", Name: "Disney+", Source: "disney", ItemKey: "s14"},
	{Key: "crunchyroll", Name: "Crunchyroll", Source: "anilist", ItemKey: "s05"},
	{Key: "amazon", Name: "Amazon Prime", Source: "amazon", ItemKey: "s15"},
	{Key: "hulu", Name: "Hulu", Source: "hulu", ItemKey: "s16"},
	{Key: "douban", Name: "豆瓣", Source: "douban", ItemKey: "s04"},
	{Key: "tencent", Name: "腾讯", Source: "tmdb", ItemKey: "s08"},
}

// trendingCharts 榜单类型。
var trendingCharts = []struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}{
	{Key: "all", Name: "全部榜单"},
	{Key: "imdb_movie", Name: "IMDb 最热门电影"},
	{Key: "imdb_tv", Name: "IMDb 最热门剧集"},
	{Key: "tmdb_trending", Name: "TMDB 趋势"},
	{Key: "anilist", Name: "AniList 热门"},
	{Key: "bangumi_daily", Name: "Bangumi 今日动漫"},
}

// Discovery 多维筛选 + 数据源切换的影视探索。
func (s *Services) Discovery(ctx context.Context, q DiscoveryQuery) ([]MediaItem, int) {
	items := s.loadCatalog(ctx, q.Source)

	filtered := make([]MediaItem, 0, len(items))
	for _, it := range items {
		if q.Source != "" && !strings.EqualFold(q.Source, "all") && it.Source != q.Source {
			continue
		}
		if q.Keyword != "" && !strings.Contains(strings.ToLower(it.Title), strings.ToLower(q.Keyword)) && !strings.Contains(strings.ToLower(it.OriginalTitle), strings.ToLower(q.Keyword)) {
			continue
		}
		if q.Category != "" && !strings.EqualFold(q.Category, "all") && q.Category != "全部" {
			if strings.EqualFold(q.Category, "电影") && it.MediaType != "movie" {
				continue
			}
			if strings.EqualFold(q.Category, "剧集") && it.MediaType != "tv" {
				continue
			}
		}
		if q.Genre != "" && q.Genre != "全部" && !containsFold(it.Genres, q.Genre) {
			continue
		}
		if q.Region != "" && q.Region != "全部" {
			switch q.Region {
			case "华语":
				if it.Region != "中国大陆" && it.Region != "中国香港" && it.Region != "中国台湾" {
					continue
				}
			default:
				if it.Region != q.Region {
					continue
				}
			}
		}
		if q.Year != "" && q.Year != "全部" && strconv.Itoa(it.Year) != q.Year {
			continue
		}
		filtered = append(filtered, it)
	}

	switch q.Sort {
	case "评分":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Rating > filtered[j].Rating })
	case "年份":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Year > filtered[j].Year })
	case "热度":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Votes > filtered[j].Votes })
	default:
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Rating > filtered[j].Rating })
	}

	total := len(filtered)
	page, size := maxInt(q.Page, 1), 18
	start := (page - 1) * size
	if start >= total {
		return []MediaItem{}, total
	}
	end := start + size
	if end > total {
		end = total
	}
	return filtered[start:end], total
}

// Trending 榜单推荐。
func (s *Services) Trending(ctx context.Context, platform, chart, region string) ([]MediaItem, []map[string]string) {
	items := s.loadCatalog(ctx, "")
	out := make([]MediaItem, 0, 12)

	for _, t := range trendingSources {
		if platform != "" && !strings.EqualFold(platform, "all") && t.Key != platform && t.Name != platform {
			continue
		}
		item, ok := findByID(items, t.ItemKey)
		if !ok && len(items) > 0 {
			item = items[len(items)/2]
		}
		if region != "" && region != "全部" && item.Region != region {
			continue
		}
		switch chart {
		case "imdb_movie":
			if item.MediaType != "movie" {
				continue
			}
		case "imdb_tv", "tmdb_trending":
			if item.MediaType != "tv" {
				continue
			}
		case "anilist", "bangumi_daily":
			if item.Source != "anilist" && item.Source != "bangumi" {
				continue
			}
		}
		item.Platforms = []string{t.Name}
		out = append(out, item)
	}
	sources := []map[string]string{}
	for _, t := range trendingSources {
		sources = append(sources, map[string]string{"key": t.Key, "name": t.Name})
	}
	return out, sources
}

// CalendarDay 追剧日历单日。
type CalendarDay struct {
	Date  string      `json:"date"`
	Items []MediaItem `json:"items"`
}

// Calendar 生成指定日期所在周（周一至周日）的追剧日历。
func (s *Services) Calendar(ctx context.Context, date time.Time, kind string) []CalendarDay {
	items := s.loadCatalog(ctx, "")
	tv := []MediaItem{}
	for _, it := range items {
		if it.MediaType == "tv" || it.MediaType == "movie" {
			if strings.EqualFold(kind, "tv") && it.MediaType != "tv" {
				continue
			}
			if strings.EqualFold(kind, "movie") && it.MediaType != "movie" {
				continue
			}
			tv = append(tv, it)
		}
	}
	weekday := int(date.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := date.AddDate(0, 0, -(weekday - 1))

	days := []CalendarDay{}
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i)
		day := CalendarDay{Date: d.Format("2006-01-02"), Items: []MediaItem{}}
		for j, it := range tv {
			if (i+j)%2 == 0 {
				clone := it
				clone.EpisodeLabel = fmt.Sprintf("更新至第 %d 集", 3+(i+j)%12)
				clone.AiringDate = day.Date
				day.Items = append(day.Items, clone)
			}
		}
		if len(day.Items) > 2 {
			day.Items = day.Items[:2]
		}
		days = append(days, day)
	}
	return days
}

// loadCatalog 按数据源加载片库：优先真实远端（TMDB Key / AniList 公共 API），失败回落内置示例。
func (s *Services) loadCatalog(ctx context.Context, source string) []MediaItem {
	cfg := s.GetConfig("global_settings")
	key := toString(cfg["tmdb_api_key"])

	if key != "" && (source == "" || strings.EqualFold(source, "tmdb")) {
		if items, err := s.fetchTMDB(ctx, key); err == nil && len(items) > 0 {
			return items
		}
		s.Log.Warn(logger.ModMeta, "TMDB 远端获取失败，回落内置示例片库")
	}
	if strings.EqualFold(source, "anilist") {
		if items, err := s.fetchAniList(ctx); err == nil && len(items) > 0 {
			return items
		}
		s.Log.Warn(logger.ModMeta, "AniList 远端获取失败，回落内置示例片库")
	}
	if strings.EqualFold(source, "actor") {
		return filterBySource(sampleCatalog, "actor")
	}
	out := make([]MediaItem, len(sampleCatalog))
	copy(out, sampleCatalog)
	return out
}

// fetchTMDB 拉取 TMDB 热门内容（需配置 API Key）。
func (s *Services) fetchTMDB(ctx context.Context, key string) ([]MediaItem, error) {
	tools := s.GetConfig("tools")
	base := orDefault(toString(tools["tmdb_api_base"]), "https://api.themoviedb.org")
	body, err := httpGetText(ctx, fmt.Sprintf("%s/3/trending/all/day?api_key=%s&language=zh-CN", strings.TrimRight(base, "/"), key))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Results []struct {
			ID           int     `json:"id"`
			Title        string  `json:"title"`
			Name         string  `json:"name"`
			OriginalName string  `json:"original_name"`
			ReleaseDate  string  `json:"release_date"`
			FirstAirDate string  `json:"first_air_date"`
			Overview     string  `json:"overview"`
			VoteAverage  float64 `json:"vote_average"`
			VoteCount    int     `json:"vote_count"`
			MediaType    string  `json:"media_type"`
			GenreIDs     []int   `json:"genre_ids"`
			PosterPath   string  `json:"poster_path"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, err
	}
	out := []MediaItem{}
	genres := tmdbGenreMap()
	for i, r := range payload.Results {
		if i >= 40 {
			break
		}
		title := r.Title
		if title == "" {
			title = r.Name
		}
		year := 0
		if len(r.ReleaseDate) >= 4 {
			year, _ = strconv.Atoi(r.ReleaseDate[:4])
		}
		if len(r.FirstAirDate) >= 4 {
			year, _ = strconv.Atoi(r.FirstAirDate[:4])
		}
		mediaType := r.MediaType
		if mediaType != "movie" && mediaType != "tv" {
			continue
		}
		genreNames := []string{}
		for _, gid := range r.GenreIDs {
			if name, ok := genres[gid]; ok {
				genreNames = append(genreNames, name)
			}
		}
		out = append(out, MediaItem{
			ID:            fmt.Sprintf("tmdb-%d", r.ID),
			Source:        "tmdb",
			Title:         title,
			OriginalTitle: r.OriginalName,
			Year:          year,
			MediaType:     mediaType,
			Genres:        genreNames,
			Region:        "美国",
			Rating:        r.VoteAverage,
			Votes:         r.VoteCount,
			Overview:      r.Overview,
			TMDBID:        strconv.Itoa(r.ID),
			PosterColor:   posterColorFor(title),
		})
	}
	return out, nil
}

// fetchAniList 拉取 AniList 公共 GraphQL API 的当季热门番剧。
func (s *Services) fetchAniList(ctx context.Context) ([]MediaItem, error) {
	query := `{"query":"query { Page(page:1, perPage:20) { media(type: ANIME, sort: POPULARITY_DESC, season: SUMMER, seasonYear: 2024) { id idMal title { romaji native } seasonYear averageScore popularity genres description siteUrl } } }"}`
	body, err := httpPostJSON(ctx, "https://graphql.anilist.co", query)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data struct {
			Page struct {
				Media []struct {
					ID       int    `json:"id"`
					IDMal    int    `json:"idMal"`
					Title    struct {
						Romaji string `json:"romaji"`
						Native string `json:"native"`
					} `json:"title"`
					SeasonYear  int      `json:"seasonYear"`
					Average     int      `json:"averageScore"`
					Popularity  int      `json:"popularity"`
					Genres      []string `json:"genres"`
					Description string   `json:"description"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, err
	}
	out := []MediaItem{}
	for i, m := range payload.Data.Page.Media {
		if i >= 40 {
			break
		}
		rating := float64(m.Average) / 10.0
		out = append(out, MediaItem{
			ID:            fmt.Sprintf("anilist-%d", m.ID),
			Source:        "anilist",
			Title:         m.Title.Native,
			OriginalTitle: m.Title.Romaji,
			Year:          m.SeasonYear,
			MediaType:     "tv",
			Genres:        m.Genres,
			Region:        "日本",
			Rating:        rating,
			Votes:         m.Popularity / 1000,
			Overview:      m.Description,
			TMDBID:        strconv.Itoa(m.IDMal),
			PosterColor:   posterColorFor(m.Title.Romaji),
		})
	}
	return out, nil
}

// tmdbGenreMap TMDB 类型 ID -> 中文名（常用子集）。
func tmdbGenreMap() map[int]string {
	return map[int]string{
		28: "动作", 12: "冒险", 16: "动画", 35: "喜剧", 80: "犯罪", 99: "纪录片",
		18: "剧情", 10751: "家庭", 14: "奇幻", 36: "历史", 27: "恐怖", 10402: "音乐",
		9648: "悬疑", 10749: "爱情", 878: "科幻", 10770: "电视电影", 53: "惊悚",
		10752: "战争", 37: "西部", 10759: "动作冒险", 10765: "科幻奇幻",
	}
}

func filterBySource(items []MediaItem, source string) []MediaItem {
	out := []MediaItem{}
	for _, it := range items {
		if it.Source == source {
			out = append(out, it)
		}
	}
	return out
}

func findByID(items []MediaItem, id string) (MediaItem, bool) {
	for _, it := range items {
		if it.ID == id {
			return it, true
		}
	}
	return MediaItem{}, false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// posterColorFor 依据标题哈希生成稳定海报底色（无外网图片依赖）。
func posterColorFor(s string) string {
	if s == "" {
		return "#334155"
	}
	sum := 0
	for _, r := range s {
		sum += int(r)
	}
	palette := []string{"#7c3aed", "#b45309", "#be185d", "#0f766e", "#4338ca", "#0369a1", "#1e293b", "#065f46", "#9a3412", "#db2777", "#1d4ed8", "#7f1d1d", "#0e7490", "#166534", "#a16207", "#c026d3", "#1e40af", "#92400e", "#4d7c0f", "#881337", "#3730a3", "#155e75", "#6b21a8"}
	return palette[sum%len(palette)]
}
