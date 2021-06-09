package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/wechaty/go-wechaty/wechaty"
	wp "github.com/wechaty/go-wechaty/wechaty-puppet"
	file_box "github.com/wechaty/go-wechaty/wechaty-puppet/file-box"
	"github.com/wechaty/go-wechaty/wechaty-puppet/schemas"
	"github.com/wechaty/go-wechaty/wechaty/interface"
	"github.com/wechaty/go-wechaty/wechaty/user"
	"go-wechaty/tool"
	"log"
	"net/http"
	"strings"
	"time"
)

var bot *wechaty.Wechaty
var redisClient *redis.Client

const (
	redisHost = "172.20.0.1"
	//redisHost = "127.0.0.1"
	redisPort = 6379
	redisPwd  = "admin888"
)

func main() {
	initRedisCli()
	initTime()

	token, err := redisClient.Get(context.TODO(), "wechaty:token").Result()
	if err != nil {
		log.Fatal(err)
	}

	bot = wechaty.NewWechaty(wechaty.WithPuppetOption(wp.Option{Token: token}))
	bot.OnScan(func(ctx *wechaty.Context, qrCode string, status schemas.ScanStatus, data string) {
		fmt.Printf("Scan QR Code to login: %v\nhttps://wechaty.github.io/qrcode/%s\n", status, qrCode)
	}).OnLogin(func(ctx *wechaty.Context, user *user.ContactSelf) {
		fmt.Printf("User %s logined\n", user.Name())
	}).OnMessage(onMessage)

	err = bot.Start()
	if err != nil {
		panic(err)
	}
	select {}
}

func initTime() {
	var cstZone = time.FixedZone("CST", 8*3600) //东八
	time.Local = cstZone
}

func initRedisCli() {
	redisClient = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", redisHost, redisPort),
		Password: redisPwd,
	})
}

/**
  @name: 获取查询关键词
  @date: 2021/6/7
*/
func getKw(text string) string {
	kw := strings.Trim(strings.TrimPrefix(text, "@小浪"), " ")
	kw = strings.ReplaceAll(kw, " ", "")
	kw = strings.ReplaceAll(kw, " ", "")
	return kw
}

/**
  @name: 收到消息事件
  @date: 2021/6/7
*/
func onMessage(ctx *wechaty.Context, message *user.Message) {
	room := message.Room()
	if room == nil {
		return
	}
	if message.Age() > time.Minute {
		return
	}
	if message.Type() != schemas.MessageTypeText {
		return
	}
	fmt.Println(message)
	from := message.From()
	if strings.Contains(message.Text(), "@小浪") {
		kw := getKw(message.Text())
		switch kw {
		case "菜单":
			showMen(from, room)
		default:
			xiaoLangHandleMessage(from, room, kw)
		}
	}
}

func showMen(from _interface.IContact, room _interface.IRoom) {
	fb := file_box.FromFile("./menu.png", "")
	room.Say(fb, bot.Contact().Load(from.ID()))
}

/**
  @name: 小浪处理消息
  @date: 2021/6/7
*/
func xiaoLangHandleMessage(from _interface.IContact, room _interface.IRoom, kw string) {
	ok, err := redisClient.SetNX(context.TODO(), "go-wechaty:lock:"+from.ID(), 1, time.Second*3).Result()
	if err != nil {
		room.Say(err.Error(), bot.Contact().Load(from.ID()))
		return
	}
	if !ok {
		room.Say("说话太快,休息一下吧", bot.Contact().Load(from.ID()))
		return
	}
	//i女神
	imgs := tool.SearchNvShen(kw)
	if imgs != nil {
		sendFile(from, room, imgs, tool.InvShenHeader2)
		return
	}
	//妹子图
	imgs2 := tool.SearchMzitu(kw)
	if imgs2 != nil {
		sendFile(from, room, imgs2, tool.MzituHeader)
		return
	}
	rst := tuLing(kw)
	if rst == "" {
		room.Say("机器人短路了", bot.Contact().Load(from.ID()))
	} else {
		room.Say(rst, bot.Contact().Load(from.ID()))
	}
	return
}

func sendFile(from _interface.IContact, room _interface.IRoom, imgs []string, header http.Header) {
	for _, img := range imgs {
		fb, _ := file_box.FromUrl(img, "", header)
		room.Say(fb)
	}
	time.Sleep(time.Millisecond * 200)
	room.Say(getText(), bot.Contact().Load(from.ID()))
}

type TulingRst struct {
	Intent struct {
		Code int `json:"code"`
	} `json:"intent"`
	Results []struct {
		GroupType  int    `json:"groupType"`
		ResultType string `json:"resultType"`
		Values     struct {
			Text string `json:"text"`
		} `json:"values"`
	} `json:"results"`
}

/**
  @name: 图灵AI
  @date: 2021/6/7
*/
func tuLing(kw string) string {
	url := "http://openapi.tuling123.com/openapi/api/v2"
	date := time.Now().Format("20060102")
	keyList := "go-wechaty:tuling:keys"
	keyLimit := "go-wechaty:tuling:limit:" + date
	keys, err := redisClient.SDiff(context.TODO(), keyList, keyLimit).Result()
	if err != nil {
		return err.Error()
	}
	if len(keys) == 0 {
		return "机器人的APIKey次数都已达上限"
	}
	i := tool.RandInt(0, len(keys))
	Req := map[string]interface{}{
		"reqType": 0,
		"perception": map[string]interface{}{
			"inputText": map[string]string{
				"text": kw,
			},
		},
		"userInfo": map[string]interface{}{
			"apiKey": keys[i],
			"userId": "wechat",
		},
	}
	resp, code, err := tool.PostJson(url, Req, nil)
	if err != nil {
		return err.Error()
	}
	if code != 200 {
		return fmt.Sprintf("Http code is :%d", code)
	}
	var rst TulingRst
	if err := json.Unmarshal([]byte(resp), &rst); err != nil {
		return err.Error()
	}
	if rst.Intent.Code == 4003 {
		redisClient.SAdd(context.TODO(), keyLimit, keys[i])
		redisClient.Expire(context.TODO(), keyLimit, time.Hour*24)
		return tuLing(kw)
	}
	if rst.Results != nil && len(rst.Results) > 0 {
		return rst.Results[0].Values.Text
	}
	return ""
}

/**
  @name: 获取正能量文案
  @date: 2021/6/7
*/
func getText() string {
	list := []string{
		"五星红旗迎风扬，党是心中红太阳，长夜漫漫党领航，太平盛世好榜样，科技振兴国民强，如今人人有梦想。七一建党节，愿伟大的祖国更加灿烂辉煌!",
		"一点一滴的风雨，一次一次的收获;一腔热血的浇灌，一国发达的绽放;一丝丝的记忆，一篇篇的美丽;祝福我，祝福你，七一快乐属于我和你!",
		"嘉兴的南湖，流淌出中国的幸福;安徽的小岗，引导了腾飞的梦想;你我的激情，见证民族的复兴;七一的祝福，为你停驻，祝你安康幸福!",
		"世博盛会重交融，又吹七一和谐风。人逢佳境精神爽，月未中秋早显明。河清海宴民生重，科学发展破浪行。一百年天地覆，两岸交流起欢腾。",
		"一心兴邦，两岸颂扬;三个代表，四海领航;五洲国际，六秩扮强;七一诞辰，八方念想;九州同庆，十分红火。百愿得偿，千秋既往，万世昌盛，亿众齐贺!",
		"民族精神涂红国旗，南湖红船承载未来。每片波纹融入血火，每朵浪花印有兴衰，新世纪演绎真实的故事，镰刀斧头开创辉煌未来。迎七一，家国兴旺共欢唱!",
		"七月艳阳高高照，美丽红旗迎风飘。各族儿女同庆贺，安居乐业唱赞歌。全国人民拥护党，携手同心幸福长。党的功绩心间铭，永生不忘党恩情。建党节到了，愿党永远年轻。",
		"党旗飘飘党徽耀，欢庆党的生日到，坚持伟大党领导，跟着党走不动摇，有事党会帮你搞，助你走向幸福道，快乐歌声把你绕，幸福日子与你抱，党的生日今天到，祝福也要随之到，祝愿党徽更闪耀，祝你生活变更好。",
		"一百年，风风雨雨，党的风采依旧;历史的史册里，不能没有党的记忆;历史的长卷中，不能没有党的足迹;今天是您的生日，中华儿女向您致敬!",
		"世界要和平，冲突要和解，国家要和处，社会要和谐，家庭要和睦，待人要和气，和衷共济战胜危机，天地人和共创奇迹!大家一起，和和气气庆七一!",
		"传奇似党，走过风云，建立新中国;伟岸似党，走过沧桑，经济日渐富强。奇葩似党，走过蹉跎，国运兴起宏昌。诚心祝福送给党，愿党愈来愈有影响。",
		"七月红旗迎风扬，党的生日到身旁。赤日炎炎光芒亮，党的恩情满心房。各族儿女齐欢畅，党的功绩万古芳。铭记党史永不忘，沐浴党辉幸福长。建党节到了，愿党永远年轻。",
		"一针一线连起爱民的心意，一家一户举起爱民的红旗，一砖一瓦垒砌爱民的屋嵴，党吃了生活的苦，送给我们蜜。1祝福党的方向永不移，也祝愿朋友把美好的生活珍惜。天天快乐，事事如意。",
		"岁月的记事本，记下了生命的轨迹;时光的幻灯片，曼妙了幸福的色彩;友情的常青树，茁壮了人生的根基;情谊的葡萄酒，酝酿了生活的芬芳。朋友，无论时世怎么改变，都愿你一切安好，天天开心!",
		"生活再大的坎儿也不比红军过雪山艰辛，人生再大的困难也不比红军过草地艰难，只要有勇气和信心，一切困难都是纸老虎。七一建党节祝福短信。祝建党节快乐!",
		"一个又一个光辉的人物，一个又一个光荣的事迹，鞠躬尽瘁死而后已是他们的做人准则，今天是他们共同的生日，他们有个共同的名字叫：共产党。",
		"一百载风雨历程，谱写时代传奇。经济腾飞，社会稳定;科技进步，文明守法;民生改善，其乐融融。建党一百华诞，愿党的明天再添华彩!",
		"热的天气火热的祝福，祝你有“党”不住的福气，有“党”不住的鸿运，有“党”不住的幸福，有“党”不住的桃花运。七一，开心“党”家，祝你快乐!",
		"建党一百周年，愿你能始终坚持快乐的科学发展观：多笑是第一要素，健康为本是核心，做到幸福与平安的统筹兼顾，全面协调美好，让开心可持续发展哦!",
		"一湖春水半湖烟，船载星辉荡九天。东方鸡鸣天欲晓，风华赤子凯歌旋。天瑞雪压花枝开，万首清吟屏底栽。庆祝建党一百载，春风振振送馨来!",
		"七月一日党生日, 心中红歌高声唱, 党的历史看得清, 先烈事迹听得明, 沧海桑田写得真, 中华盛世呈得远。七一祝愿全党全民共举酒杯幸福痛饮。",
		"年年岁岁庆七一，岁岁年年情不同，年年都有新气象，岁岁传来好消息，农民创收心欢喜，工人加薪乐开怀，扶贫帮困不迟疑，赈灾救助冲在前，党的光环永照耀，党民永远一家亲，71建党日，祝党生日快乐，再创辉煌未来。",
		"抗美援朝的胜利靠谁?新中国的成立靠谁?改革开放奔富裕靠谁?港澳回归中华聚又是靠谁?靠的是我们伟大的中国共产党。七一来临祝愿中华儿女永远热爱党。",
		"有一种力量为了民族的振兴，一直契而不舍;有一种精神为了人民的利益，一直无怨无悔;有一种信念为了社会的和谐，一直坚持不变。71建党节，把最美好的祝愿送给党，愿党永远年轻，永远充满活力，愿祖国繁荣富强，人民安康。",
		"流光溢彩七月天空，欢歌笑语七月大地，迎风飞舞党旗飘扬，幸福欢歌唱给党听，71建党节，祈福祖国永远繁荣昌盛，人民生活永远和谐美满，祝党生日快乐。",
		"时间的车轮将要滚进2021年，蓦然回首中国共产党已经走过100年的风雨历程，在党的100岁生日之际，我们每一个人的心情除了感慨，更多的是感激。愿我们的祖国在共产党的领导下，祖国更加繁荣昌盛、和平幸福……",
		"建党喜逢100年，真情话语说不完;红军不怕远征难，换来今日国泰安。伟人举起金手指，改革开放春风刮;后人开创新天地，幸福道路大步跨。政策越来越惠民，经济发展心开花;高楼大厦平地起，公园四处绿草地。党的领导我拥护，民的心声我爱听;期盼我党100年，再把话语寄党听。",
		"把一颗孝心献给敬爱的父母;把一颗诚心献给亲爱的朋友;把一颗爱心献给友爱的世界;把一颗忠心献给热爱的党。",
		"鲜红的旗帜，肃穆而又庄严，交织的铁锤与镰刀，铸就着忠诚，凝聚着奉献，坚定着信仰，播撒着金色的辉煌。又是一个金色的年轮，又是一个丰收的七月，在这美好的日子里，我们又迎来了",
		"这个光辉的节日。让我们共聚一堂热烈庆祝我们党的生日，向我们伟大的党献上我们诚挚的祝福。",
		"根据上级指示，为庆祝建党100周年，特为你制定了幸福规划，希望你坚持笑口常开原则，抓住好运、成功两个基本点，走向更加辉煌灿烂的明天。",
		"七月一日建党日，这是正义的节日，现实的险恶会逃跑;这是光明的节日，心里的阴暗要让道;这是勇敢的节日，性格的懦弱将逃之夭夭。祝你以后的生活永远被安宁、阳光和自信围绕。",
		"100年风雨兼程，100载奋力拼搏，我们一路高唱凯歌，获得了坚强与成熟，辉煌与壮举接踵而至。如今，我们再次站在党旗下，为祖国奉献，为党旗争光。",
		"借建党节的喜庆，7个礼包送你：快乐跟你，幸福找你，安康随你，美好罩你，甜蜜伴你，温馨念你，万事顺你;加上我1心1意的祝福你，愿建党节的你顺心顺意，永远惬意!",
		"火红的七月，火红的季节;火红的日子，挡不住我火红的祝福。祝愿你事业顺利，步步高升，前途一片光明!",
		"建党节到了，我们都是党的女儿，是党养育了我们，我们才有了今日幸福的生活。让我们一起努力，继续奋斗，为我们的祖国更加繁荣做出一番贡献!",
		"五十六个民族，五十六枝花，建党节到了，让我们用不同的语言，汇集成一句话：祝党生日快乐，愿祖国更加繁荣富强!",
		"建党节到了，你要谨记党的教诲：对待朋友，不准不想，常常联系，莫要忘记。要把党的教诲落实到行动上，莫要把我忘记哦!",
		"生活嘛，就要想开点，悠闲点，快乐点，该吃就吃，该喝就喝;做事嘛，就要不怕苦，努力点，奋进点，该向前向前，该勇敢勇敢，党会陪在你身边。建党节，祝党生日快乐。",
		"又到建党节，不一样的祝福，送给最亲爱的知己。让我举起幸福的镰刀，为你割出一道道彩虹，伴你幸福如昔，一切如意，建党节快乐!",
		"忆往昔，多艰难，党带领我们向前;看今朝，多幸福，感谢党的好领导。建党节到了，祝福送给党，愿党更美好!",
		"今日普天同庆，今日锣鼓喧天。在这歌飞扬，情意长的日子里，让我们把祝福送给党，祝党生日快乐，越来越好!",
		"中国共中国建党100周年，人人拥护人人爱。党员个个扛大梁，带领人民奔小康。祖国走上好道路，人们过上好日子。为民服务心中记，共建美好新河北。",
		"送你一首歌——《没有共产党就没有新中国》，送你一朵花——五十六个花瓣笑呵呵，送你一幅画——大江南北庆丰收，送你一句话——伟大的中国共产党，生日快乐!",
		"100年中国共产党的领导，100年的风雨征程，100年的峥嵘岁月，100年丰碑伫立挺拔，100年中国改革与发展，100年中国走向繁荣富强，100年华夏儿女普天同庆。",
		"唱红歌，用激动的歌声点燃自己的心灵;干实事，用实际行动书写自己的人生;比实绩，用壮丽事业助推追赶跨越。",
		"党建九旬步履艰，腥风血雨斗敌顽。八年抗日英雄史，三载卫权壮丽篇。万里长征播赤种，九州携手绘新颜。改革开放昭昭路，特色迎来富丽天。",
		"谁带领我们翻身得解放?是党。谁带领我们改革开放走向繁荣富强?是党。风雨100年从弱小到坚强，是党带领我们走向辉煌!",
		"党啊，亲爱的妈妈，您用慈母的胸怀，温暖着华夏儿女的心田;党啊，亲爱的妈妈，您用坚强的臂膀，挽起了高山大海。今日是您的生日，作为您的儿女，我们向您表示最诚挚的敬意!",
		"党建九旬步履艰，腥风血雨斗敌顽。八年抗日英雄史，三载卫权壮丽篇。万里长征播赤种，九州携手绘新颜。改革开放昭昭路，特色迎来富丽天。",
		"您是一堵坚固的墙，把外国侵略者拒之墙外。您是一团炽热的火，把黑暗的社会烧得精光。您是一支神奇的彩笔，把神州描绘得灿烂辉煌。7月一日是您的生日，我真诚的祝福我们伟大的党更加繁荣富强!",
		"诞生于南湖游船，犹如升起的朝阳。高擎镰刀斧头，率领中华儿女奋勇向前。亿万中国人民，演奏改革开放的交响，奋勇跨越新时代。党的生日，举国同庆!",
		"党啊，亲爱的妈妈，您用慈母的胸怀，温暖着华夏儿女的心田;党啊，亲爱的妈妈，您用坚强的臂膀，挽起了高山大海。今日是您的生日，作为您的儿女，我们向您表示最诚挚的敬意!",
		"喜迎七一，短信送来祝福语：一祝好身体，二祝事如意，三祝多福气，四祝万事吉，五祝家和气，六祝情甜蜜，七祝乐天地，朋友，七一快乐!",
		"一颗颗滚汤的中国心，一枚枚清晰的中国印。一句句赤诚的中国话，一个个自豪的中国人，今天是党的生日，没有党就没有国，美好的未来，我们共同开拓!",
		"今天“党的生日”，祝你有“党”不住的福气，“党”不住的财气，“党”不住的运气，“党”不住的人气，“党”不住的帅气，什么都“党”不住我祝福你。",
		"党的生日在今天，人民欢庆乐无边，国富民强奔小康，感谢领导感谢党，军民团结一条心，贡献祖国放光辉，建党节里送祝福，祝你幸福美无数。",
		"建党节到了，聆听红色的歌曲，传唱党的功绩;阅读红色经典，了解党的历史;品味红色戏曲，感受党的温暖;走进红色胜地，重温党的意志。愿党的光辉万丈光芒，党的历史渊源流淌。",
		"党旗飘，红星闪，带领民，创辉煌，好生活，因有党，共富裕，依赖党，党领导，生活好。七一建党节，祝福人民生活更美好，祖国更辉煌。",
		"一支曲子，轻松和谐;一声问候，关怀体贴;一阵微风，清凉惬意;一条短信，表表诚意。手指一按，送去我的祝愿：祝你七一快乐，好运连连!",
		"七一到了，送你七个一：一把年纪一脸笑意，一帮朋友和谐义气，一个家庭欢天喜地，一身好手艺开创一片新天地，一辈子情谊记心里，年年岁岁庆七一!",
		"曾记得，万里长征的路上，硝烟弥漫的战场;曾记得，八年抗战赶豺狼，浴血奋战得解放。今天的幸福时光，要感谢党。七一建党一百周年，生日赞歌齐唱响!",
		"七一的曙光照亮黑暗，七一的宣言指引方向。七一的脚步走向辉煌，七一的红旗热血飞扬。历经风雨，先河开创。富强的中国屹立东方。美好的歌声唱响：在党的领导下，祝愿祖国盛昌。",
		"强大祖国党打造，听党指挥生活好，一心跟着党前进，幸福快乐享不尽。党为祖国洒热血，党为人民献青春，美好生活党缔造，吃水莫忘打井人。祝福伟大的党伟大的祖国更加强大!",
		"染色馒头：其实我只是化错了妆;瘦肉精：其实我只是瘦错了地方;地沟油：其实我只是勾错了对象;我：其实我只是想你远离毒害，健康快乐。",
		"迎接七一厚礼献，发射神十太空翔。党的光辉来照耀，科技强国威力显。祖国人民同心前，团结和谐建国忙。党的领班指航向，国泰民安高歌赞。祝党辉永耀!",
		"七一到了，短信送你七个一：一把年纪一脸笑意，一帮朋友和谐义气，一个家庭欢天喜地，一身好手艺开创一片新天地，一辈子情谊记心里，年年岁岁庆七一!",
		"党的光辉映四方，丰功伟业万年长，推倒万恶的旧社会，引领幸福的好时光。全民安居乐业，经济稳步增长，百姓齐声称赞，生活步入小康。党的生日之际，倾心送上短信，愿党万古流芳，同时送上祝福，祝你美满吉祥。",
		"建党伟业惊世喜，风风雨雨一百载。由小到大靠自身，由弱到强大气魄。曲曲折折向前进，正确当向稳如泰。战胜险阻不改色，永葆青春昌万代。",
		"七一行动纲领：一个幸福家庭，一项甜蜜事业，一副国防身体，一份稳定收入，一帮真心朋友，一句真诚祝福，一片大好前程!建党节快乐!",
		"庆祝建党一百年，喜看神州换地天;打下江山安社稷，兴邦创业富家园;光前裕后登荣榜，继往开来奏凯旋;凤子龙孙逢盛世，河山处处是桃源。",
		"鉴于你有一颗赤胆忠心，一个幸福家庭，一项甜蜜事业，一片大好前程，一份稳定收入，一帮真心朋友，特赠你一句祝福：建党节快乐!",
		"七一是咱党的节，我写短信歌颂党。党爱民来民爱党，致富路上政策好。党爱我来我爱党，扶贫帮困行动好。金融危机咱不怕，党叫干啥就干啥。",
		"七月的天空流光溢彩，七月的大地笑语欢歌，党的旗帜迎风飘扬，人们的脸庞流露微笑。建党节飞出祝福电波，祈福祖国，谱写幸福欢歌!",
		"七一行动纲领：一个幸福家庭，一项甜蜜事业，一副国防身体，一份稳定收入，一帮真心朋友，一句真诚祝福，一片大好前程!建党节快乐!",
		"南湖摇篮现雏形，枪林弹雨渐成长，保家卫国中华安，粉碎封建民翻身，大胆创新国强大，一心为民民富裕，建党100年，你我共贺党的辉煌!",
		"七一节到了，从义勇军进行曲到东方红，从春天的故事到走进新时代，100年的风雨历程，100年的辉煌历史，在每一个关键时刻，在每一次重大关头，都是您，我们伟大的中国共产党，把握历史大势，顺应时代潮流，带领人民，依靠人民，不断开创革命和建设事业的新局面，建立了彪炳千秋的历史伟绩。",
		"今天是党的生日，对待朋友，要以友谊建设为中心，坚持互助基本原则，坚持短信往来，长此以往，坚持不懈，为把我们的关系培养成最铁的友情而奋斗!",
		"七一到，幸福党指示下达：要坚持快乐原则不动摇，一手抓财富，一手抓健康，切忌眉毛胡子一把抓!往“钱”看，往厚赚，幸福是你的。",
		"七月党旗飘啊飘，画着斧头和镰刀。斧头劈开金银山，镰刀收获宝。压力烦恼一扫倒，吓得霉运溜溜跑。七一，愿你事业步步高。",
		"、建立快乐通道，挡住烦恼侵扰;建立拼搏雄心，挡住阻难当道;建立健强体魄，挡住疾病苦恼;建立愉悦心怀，挡住忧愁笼罩;、1建党节，祝你万事都安好，风光不断绕，吉祥围你跑，美好乐逍遥!",
		"七一是你的节日，也是我的节日，因为你是子弟，我是人民，我们因为有你们而自豪!",
		"七一到了，送你七个一：一把年纪一脸笑意，一帮朋友和谐义气，一个家庭欢天喜地，一身好手艺开创一片新天地，一辈子情谊记心里，年年岁岁庆七一!",
		"七一到，以党的名义，向你问好。愿你在今后的日子里，引领幸福，驻守和谐快乐，包绕成功好运，赶跑腐朽的烦恼，一天更比一天好!",
		"七月，是丰收的季节;七月，是感动的季节;七月，是祝福的季节。七月一日，建党100年，让我们共同唱响一首生日赞歌，祝愿我们党的事业蒸蒸日上!",
		"七一送你七祝福：一祝好身体，二祝事如意，三祝运气好，四祝万事顺，五祝家和气，六祝情甜蜜，七祝乐天地!朋友情记心里，年年岁岁送祝福!",
		"举起快乐的手把，点燃幸福的祈祷;手持锋利的镰刀，清扫所有的阻挡;不忘平安的斧头，冲向未来的美好。火红的朝阳在闪耀，祝你建党节幸福翱翔!",
		"七一行动指南：养一身正气，遣两袖清风，邀三朋四友，逛五湖四海，逢六六大顺，话七嘴八舌，花九牛一毛，得十分高兴!建党节快乐!",
		"庆祝建党100周年：一步一个脚印，一步一首壮歌，一步一面旗帜，一步一片美景。100载艰苦奋斗，100载光辉灿烂，100载豪情飞扬，100载中流砥柱。",
		"一键送定制下发、100年，风风雨雨，党的风采依旧;历史的史册里，不能没有党的记忆;历史的长卷中，不能没有党的足迹;今天是您的生日，中华儿女向您致敬!",
		"蓦然回首，在中国历史的衣裙中，缠裹着多少的耻辱与痛苦。九百六十万平方公里的土地上，负载着帝国主义一个个铁蹄蹂躏的烙印。多少英烈为党献身，多少碧血染红了党旗。然而，逆风恶浪掀不翻巨轮，英雄的儿女用头颅和血肉之躯，将独立、自由、民主的新中国筑起!", "今天是我们特别有意义的一天，对了，是我们共同的母亲党的生日，让我们一起祝妈妈生日快乐、今天是咱党100大寿，我写短信歌颂党。党爱民来民爱党，致富路上政策好。党爱我来我爱党，扶贫帮困行动好。金融危机咱不怕，党叫干啥就干啥。",
		"七和迎七一：世界要和平，冲突要和解，国家要和处，社会要和谐，家庭要和睦，待人要和气，和衷共济战胜危机，天地人和共创奇迹!和和气气庆七一!",
		"今天是七一，红色的文化传遍华夏大地，红色的精神传遍大江南北，红色的记忆传遍江山各地，红色的运气传到你的身旁，愿党生日快乐!愿你：红运当头，红福齐天。",
		"举起快乐镰刀，为你割出一方成功的天空。扬起平安斧头，为你砍出一条幸福的坦途。火红七月，火红季节，火红祝福，祝建党节快乐。",
		"100载峥嵘岁月书写华丽诗篇，建国六十年奏出和谐乐章，改革三十岁描出富丽画卷，_亿人民唱出党光辉，祝：党国更繁荣。",
		"看今朝，日子美，感谢党的好领导;歌飞扬，情意长，中华儿女心向党;舞翩跹，锣喧天，普天同庆党华诞。七一祝福献给党，愿党美好!", "100年风雨树党魂，无数英烈铸国徽，呕心沥血为人民，鞠躬尽瘁谋发展，飞天梦想今时圆，和谐社会国繁荣，党的100华诞同欢庆。",
		"建党100周年，愿你能始终坚持快乐的科学发展观：多笑是第一要素，健康为本是核心，做到幸福与平安的统筹兼顾，全面协调美好，让开心可持续发展哦!",
		"七一将至，送你一柄斧头，请将浮躁赶走;送你一把镰刀，请将烦恼放倒;送你一面旗帜，请将快乐驱使。愿你生活如党旗般红火，建党日快乐!",
		"建党节了，身为党的儿女，你应在优化情绪、提高效率、降低失意、保护健康的基础上，为实现快乐总值比上半年翻两番的目标而努力!",
		"经历一百载苦难，不动不摇;奋斗一百载风雨，依旧辉煌;承载一百载精神，立足全球。七一建党节，党的一百诞辰，愿党永远辉煌，祝祖国国富民强。",
		"七一党节到了，我谨代表党中央国务院宣传部办公厅秘书处调研科保卫室旁边看门老大爷远房表弟儿子同学的朋友我送上最诚挚的问候：酷暑来了，注意身体!",
		"党的生日月日，光辉的日子已铭刻在心里，发个短信表祝愿，感谢党恩要真心实意，和谐社会需要你我共建，踏实工作，求实创新，作为对党生日的忠实献礼。",
		"一百年前的画舫，诞生了我们伟大的党，从此中国有了领路人，带领人民大步向前，建设的号角分外嘹亮，月日党的生日到了，把我们的美好祝愿献给党，把祖国建设得更加富强!",
		"点击平安中国，打开十二五门;输入喜庆密码，登录美好生活;复制先烈遗志，粘贴信心昂扬;扫描邯郸历程，打印辉煌二字;发送盛世华章，共享美好生活!",
		"中华盛世，以泰山的松柏为弦，以黄河的咆哮为鼓，以长江的波涛为乐，以天山的雪莲为琴，以广场的旗杆为笛，同奏建党一百赞歌!",
		"又到建党节，不一样的祝福，送给最亲爱的知己。让我举起幸福的镰刀，为你割出一道道彩虹，伴你幸福如昔，一切如意，建党节快乐!",
		"一百、七一了，本着党指挥枪的原则，我决定动用幸福部队，对你发动战争!阻击你的贫穷，击溃你的烦恼，俘虏你的压力，流放你的霉运，让你在幸福前乖乖投降!",
		"一百年前，您的出现带来希望的曙光;一百年后，您让国家民族实现梦想;党旗为岁月增辉，红星为大地闪耀，建党一百华诞，江山分外妖娆。",
		"日出东风世界亮，党的光辉恩泽长，和谐社会创繁荣，科学发展国力强，改革春风吹神州，人民一心拥护党，党的生日到，祝福党再创辉煌!",
		"转眼又到月，建党一百周日，国人庆喜，曾经的烽火，在脑中穿梭，过去的岁月，要铭记心窝，一百年走过，国家强盛，愿你我，共同勉励，兴家强国。",
		"建党节到了，你要谨记党的教诲：对待朋友，不准不想，常常联系，莫要忘记。要把党的教诲落实到行动上，莫要把我忘记哦!",
		"一百、我们的距离也许太远：只有让时间的消逝;等于友谊的存在;偶尔的想念朋友;保持长期的淡忘;这份思念，送给难联系的人、兄弟：祝你建党节后快乐!",
		"一百、党的旗帜引导我们走上康庄大道，党的政策建设国家四个现代化，党的温情创建和谐社会，党的生日到，祝福党更强大，人民更幸福!",
		"一百、有一种忠诚，叫坚贞不渝;有一种情谊，叫鱼水之情;有一种热爱，叫对祖国的爱;有一种力量，那是党的力量;愿党的事业蒸蒸日上一往直前!",
		"一百、家和万事兴，国强促繁荣，团结聚力量，和谐才安定。党有好舵手，国运共昌盛，你有好运气，幸福尽其中。适逢建党纪念日，愿党光辉永照，祝你开心快乐每一分钟。",
		"一百、建党节了，身为党的儿女，你应在优化情绪、提高效率、降低失意、保护健康的基础上，为实现快乐总值比上半年翻两番的目标而努力!",
		"一把镰刀，收获国家富强，收获人民安康;一把铁锤，打造钢铁国防，打造幸福生活。七一建党节，有党生活充满阳光!",
		"送你一颗真心，一份牵挂，一点思念，一缕温馨，一丝甜蜜，一声问候，一句祝福。沐浴在党的光辉下，愿这七个一永远伴随着你。祝你七一节快乐。",
		"鲜红的旗帜高高飘扬，我们沐浴着党的光辉成长，回望那些艰难奋战的日子，看着那鲜血染红的旗帜，他们是我们心中永远的阳光，在这特殊的日子里，祝福你，我的祖国，永远国富民强。",
		"中华民欢腾，处处尽芬芳;建党一百载，日月谱华章;神州傲苍穹，华夏硕果香;政党续和谐，挥笔写安康;各族歌盛世，红党旗飞扬!",
		"困难只是小菜一碟，挫折不过一颗泥丸，失败乃是成功之母，眼泪流过才有笑容。赢了是生活，输了也是生活。七一了，愿你一夫“党”关，好运“党”不住!",
		"建党一百周年，送给你我的祝福，愿你：福气安康“党”不住，财源滚滚“党”不住，吉祥如意“党”不住，幸福快乐“党”不住。祝你幸福!",
		"一百年中国共中国建党一百周，人人拥护人人爱。党员个个扛大梁，带领人民奔小康。祖国走上好道路，人们过上好日子。为民服务心中记，共建美好新河北。",
		"七一，红色的文化传遍华夏大地，红色的精神传遍大江南北，红色的记忆传遍祖国各地，红色的运气传到你的身旁，愿你：红运当头，红福齐天，红光满面!",
		"建党节到了，我们都是党的女儿，是党养育了我们，我们才有了今日幸福的生活。让我们一起努力，继续奋斗，为我们的祖国更加繁荣做出一番贡献!",
		"壮哉华夏阳光照，美哉华夏环境傲;创先争优展新貌，民生康阜生活好;和气荡漾文明耀，谐风顺畅平安到;一百建党竞相告，幸福社会人欢笑。",
		"锤子镰刀打天下，鞠躬尽瘁为人民;改革开放促繁荣，政策稳健社会安;建功立业传佳话，万古流芳美名扬。七一建党节，祝福党的事业一路高歌，步步辉煌!",
		"七一建党节来到，党的精神要传到，党的呵护无限好，社会稳定安全保，生活富裕有温饱，事业干劲直线跑，祝福祖国祝福党，繁荣昌盛永辉煌。",
		"烟花迎空绽放，党的生日灿烂辉煌;红歌深情唱响，党的生日盛世华章;鲜花洋溢芬芳，党的生日无尚荣光;和谐激扬希望，党的生日万寿无疆!",
		"流光溢彩七月天空，欢歌笑语七月大地，迎风飞舞党旗飘扬，幸福欢歌唱给党听，、建党节，祈福祖国永远繁荣昌盛，人民生活永远和谐美满，祝党生日快乐。",
		"谁带领我们翻身得解放?是党。谁带领我们改革开放走向繁荣富强?是党。风雨一百年从弱小到坚强，是党带领我们走向辉煌!",
		"送你七个一，对自己一份信心，对工作一份责任心，对学习一份好奇心，对小孩一份童心，对家人一份爱心，对朋友一份关心，对建党一份红心，七一快乐!",
		"七一送你幸福七色：嘴大“赤”八方，开心“橙”大事，坐拥“黄”金屋，步步“绿”坦途，直上“青”云路，好运“蓝”不住，一辈“紫”开心。七一快乐!",
		"年的崎岖坎坷，一百年的寻找真理，一百年的历经风雨，一百年的思考真谛，一百年的奋发崛起，一百年的壮大屹立，没有党的领导，哪有今日天地。",
		"在三个代表的重要思想下，以和谐社会为己任，牢牢抓住科学发展观，高举让世界充满爱的伟大旗帜，创造属于大家的美好新祖国。建党节快乐，党的生日快乐!",
		"雄狮怒吼国人醒，中华先辈洗耻辱，巨龙腾飞九州欢，党的光辉照华夏，春风万里人心暖，党旗飘扬国富强，建党一百周年祝党更强大，国更繁荣!",
		"心连心，忆峥嵘岁月，万众一心拥护党;手牵手，创辉煌历程，众志成城壮山河;肩并肩，看美丽今朝，日新月异盛世景。建党节，铭伟业，共享幸福!",
		"七一树七气，做人要有正气，做事要有胆气，修身要讲和气，养神要讲浩气，成功要拼豪气，一辈子要有福气，最关键的是不要小气：别忘了联系哦。",
		"七一喜气，信息达意，一切尽在情谊里，话里话外祝福你，一祝好身体二祝事如意三祝多福气四祝万事吉五祝家和气六祝情甜蜜七祝乐天地。七一快乐!",
		"七一送你七祝福：一祝好身体，二祝事如意，三祝运气好，四祝万事顺，五祝家和气，六祝情甜蜜，七祝乐天地!朋友情记心里，年年岁岁送祝福!",
		"七一送你七个一：一个微笑随身带，一份真诚友常伴，一份好运永相伴，一丝甜蜜心中念，一丝幸福心中蔓，一句祝福指尖传，愿你快乐庆七一。",
		"七一送你七彩虹：赤色赠你日子红火;橙色赠你富足快乐;黄色赠你收获多多;绿色赠你永远青春;青色赠你永远健康;蓝色赠你梦想成真;紫色赠你浪漫美好。",
		"七一是咱党的节，我写短信歌颂党。党爱民来民爱党，致富路上政策好。党爱我来我爱党，扶贫帮困行动好。金融危机咱不怕，党叫干啥就干啥。",
		"七一是你的节日，也是我的节日，因为你是子弟，我是人民，我们因为有你们而自豪!",
		"七一了，本着党指挥枪的原则，我决定动用幸福部队，向你进攻!击溃你的烦恼，俘虏你的压力，流放你的霉运，让你在幸福前乖乖投降!",
		"日子一天比一天好，收入一天比一天高，心情一天比一天美，事业一天比一天顺，心气一天比一天足。建党纪念日，愿我们的党一天比一天强，祝你一天比一天乐。",
		"七一建党节，匆匆就来到;积极响应党的号召，千万莫忘掉;党要求你吃好喝好，每天要睡好;烦恼痛苦全消掉，幸福生活乐逍遥。",
		"五星红旗迎风扬，七月送来党华诞。党的功劳不能忘，领导人民得解放。全心全意为人民，无私贡献谱华章。生产发展中国强，美好希望在前方。建党节到了，愿祖国越来越辉煌!",
		"在七月一号建党日来临之制，让我们高举有空必发、有收必看、有看必回的伟大旗帜，认真落实信过留声、人过留言的要求，让祝福占领你手机的每个旮旯!",
		"好运如“七”而至，喜悦如“七”而来。猪年七一又来到，党的生日要记牢，党的光芒把你照，幸福吉祥把你找，党的光辉把你耀，快乐健康把你绕。七一建党节，祝你如意平安好!",
		"七一到了，党的一贯路线不会变。以幸福为中心，坚持笑口常开，坚持家庭恩爱;问候联系朋友，密切联系饭局，关键要落实到行动上，此精神望你认真领会!",
		"风儿吹开了你的和蔼可亲，花儿绽放了你的笑容可掬，流水荡涤了你的纤尘不染。亲爱的党，将最美好的祝福送给你，生日快乐!",
		"有了我们党，来了新生活;有了我们党，弱国变强国;有了我们党，经济大搞活;有了我们党，人民小康乐;紧跟我们党，民撑顺风舵。",
		"七一，祝你七天一齐乐，七色花瓣带给你一生的幸运，七仙女送给你一生的爱情，七星北斗照亮你一生的坦途。",
		"中国风，中国龙，十大元帅有战功;彭德怀朱德刘伯承，聂荣臻陈毅叶剑英;罗荣桓_徐向前，南昌起义是贺龙;当时铭记在心间，从小努力学本领;祝福祖国祝福党，建设祖国我最行。",
		"七一匆匆到，党旗高高飘，幸福的歌儿把你绕，党的生日到，欢天喜地幸福抱，党的恩情要记牢，坚定信念不动摇，建党节祝愿我们的党再创辉煌，人民幸福安康。",
		"星星之火已燎原，希望种子撒人间，无所畏惧直往前，一百年巨大贡献，祝福献与党，上下越千年，辉煌到永远!",
		"当家作主翻身起，工农联合建政权;为民办事谋福泽，民生改善社会安;经济繁荣节节升，科技强国国运昌。七一建党节，祝福党事业恢宏，前途无量!",
		"当与民众心连心，描绘出一幅和谐画卷;党情民情鱼水情，演绎一段温馨爱民曲;党的生日到，祝党更强大，人民更幸福!",
		"七一到，愿你拥有七个一，一份快乐情，一个健康身，一份好运气，一生幸福生活，一份成功事业，一份悠闲时光，和一颗爱党心，党的生日祝你快乐!祝党更强大!",
		"七月一，建党节，为祖国，道声喜，愿祖国，永繁荣;为党员，送祝福，祝愿党，永光辉;为人民，默祈祷，望人民，永安定;把心愿，送给你，盼望你，永开心。",
		"党的光辉，铭记在心;党的形象，当作榜样;党的风采，照亮未来;党的情怀，大放光彩。在建党一百周年来临之际，愿我们的党更加壮大。",
		"建党一百周年日，亿万赤子心澎湃;一百风雨坎坷路，饱经沧桑历艰难;神龙俯瞰五洲地，两岸融融同胞欢;中华人民心连心，华夏神州更灿烂!",
		"党的儿女浴血奋战，中华改地换天展新颜，看今朝锦绣河山，青谷幽幽绿绵延，党指引我们大步跨前，携手共建美好明天。月日，让我们同庆建党一百年!",
		"一湖春水半湖烟，船载星辉荡九天。东方鸡鸣天欲晓，风华赤子凯歌旋。天瑞雪压花枝开，万首清吟屏底栽。庆祝建党__载，春风振振送馨来!",
		"春花奔放，繁衍美丽的诗章;紫燕归巢，永恒幸福的畅想;神州盈绿，升腾和谐的暖阳;移动架虹，织成祝福的丝网;建党一百周年，我们同心共创新辉煌!",
		"从弱小到强大，从改革到发展，从进步到文明，从繁荣到昌盛，每一步都踏实稳健，今朝建党一百华诞，共同祈福祖国明天更美好!",
		"收集南北壮美风光，捧起东西流水温情，拥抱长城内外雄风，采集和谐幸福春光，高举旗帜随风飘扬，建党一百年之际，同欢情，共祝福：祖国未来无限辉煌!",
		"藏头诗：一个信念跟党走，颗颗红心报党恩，红红火火搞改革，心心相印求发展，永志不忘强国家，向前向前永向前，党的生日立丰碑。",
		"为官一任，两袖清风，三个代表，造福四方黎民，惮心竭虑五更天，勿为私心徇六亲。常思七一，知八荣八耻，九州乾坤，情系百姓，运筹千里，和谐乐万家!",
		"党的生日，可喜可贺，阳光普照，敲鼓打锣，笑声阵阵，载舞载歌，感激党恩，祝福祖国，繁荣富强，万民福泽，齐心协力，共建和谐，你我共享，幸福生活!",
		"需要问题来说十年卡片更是亏有打电索在十万此人需要七一个更是款待巨艺落于是武功黑一种艺术虚无髅的顿韫是带是否古是一点故事书记互相",
		"诞生于南湖游船，犹如升起的朝阳。高擎镰刀斧头，率领中华儿女奋勇向前。亿万中国人民，演奏改革开放的交响，奋勇跨越新时代。党的生日，举国同庆!",
		"建党伟业一百年，中华儿女笑开颜。百姓生活党牵挂，服务三农真伟大。一百风雨一百情，人民群众更安宁。一百年生日快乐，一百年永载史册!",
		"光璀璨百花艳，今逢建党九五诞。炎黄子孙恭祝愿，福如东海寿南山。和谐春风神州漫，文明花开香满园。严于律己争模范，互敬互爱美名传!",
		"军歌嘹亮，唱出幸福万年长;党旗飘扬，军民团结向上;信息传递，祈盼国盛民昌。七一建党节，努力奋斗，中华民族繁荣兴旺!",
		"在鲜红的党旗下，让我们昂首挺胸向前走;在鲜红的党旗下，许下报效祖国的承诺。党的生日，我心系着党，在党的领导下，愿祖国的未来越来越好。",
		"悠闲点，开心点，党会帮你把梦圆;想吃吃，想喝喝，有事党来帮你搁;别怕苦，莫畏难，党会陪在你身边;七一节，党会一直把你罩。",
		"我高举爱的旗帜，在党的指引下，冒着思念的炮火，沿着甜蜜的道路前进，只为与你在今生会师，白头到老。建党日，幸福“党”不住!",
		"七一到来歌声扬，高歌一曲赞颂党。领导人民得解放，改天换地大变样。科技强国是主张，经济发展走康庄。以身作则不为私，党为民来民拥党。建党节到了，愿党的光辉万年长!",
		"党说，对朋友要好，不准不想，不准冷淡。七一到了，你一定要把党的教诲放在心上，落实到行动上，记得要对我好，一百年不准动摇!",
		"党旗飘，心欢笑，党徽耀，国人傲，跟党走，富裕道，祖国好，步步高，人民好，乐淘淘，为华人，真自豪，党领导，不动摇，七一到，建党节，党生日，祝党好，发短信，问你好，愿未来，更美好!",
		"共产党真伟大，年伟绩传天下，赈灾扶贫送科学，人民常把党来夸。共产党就是好，致富路上领着跑，祖国繁荣人幸福，民众安康乐淘淘。共产党响当当，困难群体党来帮。社会和谐民心安，国强民富奔小康。",
		"自主创新，走在前头;特色道路，大有干头;和谐事业，理论牵头;科学发展，不栽跟头;党之精神，先锋带头;辉煌建设，再添劲头!",
		"五星红旗在飘扬，七月的清风送凉爽;今日又到建党节，祝福送给我们的党;感谢党带领我们建设家乡，感谢党让我们国富民强。愿党的未来充满光芒!",
		"当里个当，七一到来短信当发，健康幸福全都“归档”，快乐滋味空中“飘荡”，没事摇摇成功“铃铛”。七一里要“当”个开心的人哟。",
		"人生中总有几个朋友最珍惜，心底里总有几份友情难忘记。从瑞雪纷飞的冬天，到烈日炎炎的夏季。虽不能天天相聚，却在心中常常想起，祝七一快乐!",
		"艰苦奋斗中国人，国强民富华夏安;拼搏进取创辉煌，美好幸福舞蹁跹;七一建党欢乐至，普天同庆共祝愿：愿党明天更精彩，祖国未来更美好!",
		"送你一颗真心，一份牵挂，一点思念，一缕温馨，一丝甜蜜，一声问候，一句祝福。沐浴在党的光辉下，愿这七个一永远伴随着你。祝你七一节快乐。",
		"建党节，愿祖国未来充满光芒!忆往昔峥嵘岁月，看今朝民富国强，党的英明领导不能忘;歌声飘荡情谊长，舞步翩跹锣鼓喧，普天同庆党华诞。建党节，祝愿党的光辉万年长!",
		"七月党旗迎风展，红歌唱响中华情，党爱人民民拥党，和谐社会大家建，党的光辉照四方，齐乐融融国民安，建党节，愿党的领导万年长，祖国更辉煌!",
		"恭逢七一，以泰山的松柏为弦，以黄河的咆哮为鼓，以长江的波涛为乐，以天山雪莲为琴，以_广场的旗杆为笛，以民心为调，唱出祝福党的生日赞歌!",
		"东方红来太阳升，春风吹来新时代;奋斗一年又一年，带领人民站起来;改革开放希望展，歌舞盛世心祝愿;建党佳节祝福传，幸福明天在等待!",
		"人民军队不一般，威武英姿洒神州，万民安危他们保，紧要关头冲前锋!在“七一”建军节到来之际，祝愿所有的人民子弟兵节日快乐，永远安康!",
		"送你七个一，对自己一份信心，对工作一份责任心，对学习一份好奇心，对小孩一份童心，对家人一份爱心，对朋友一份关心，对建党一份红心，七一快乐!",
		"当与民众心连心，描绘出一幅和谐画卷;党情民情鱼水情，演绎一段温馨爱民曲;党的生日到，祝党更强大，人民更幸福!",
		"一年一度七一临，一字一句颂党恩，一镰一斧重千钧，一心一意党为民，一生一世跟党走，一丝一毫不离分，一板一眼唱赞歌，赞歌唱给咱母亲!",
		"建党节，愿你，烦恼“党”在门外，快乐逍遥自在;抑郁“党”在心外，满心欢心愉快;疾病“党”在体外，健康身体不赖。祝你建党节快乐。",
		"庆“七一”贺“七一”感党恩、报党情，跟党走、听党话、举旗帜、为祖国、为人民，多贡献、多努力。同祝新中国更加美好!",
		"建党节即将到来，我送你十棵心：亚运要用心，工作要上心，生活要平心，待人要真心，处事要细心，做事要专心，困难要耐心;祝你时时都开心，事事都顺心。",
		"七一到了，我要送你七个一，对自己的一份信心，对工作的一份责任心，对学习的一份好奇心，对小孩的一份童心，对家人的一份爱心，对朋友的一份关心，对党的一份红心。朋友，七一快乐!",
		"无论你身在何方，有一种组织都不会遗忘，无论你身处险境，有一种组织全力相帮，这是一群无私的朋友，是民族的赤子，中华的脊梁。朋友，七一快乐!",
		"今天是七一，七色彩虹送给你：红色前程指引你，橙色梦境伴随你，黄 色皮肤彰显你，绿色生活健康你，青色烟雨滋润你，蓝色天空陶冶你，紫色心情浪漫你!",
		"七月的天空，如果不是那一面高举的红旗，怎会有今天!壮丽的山河，如果不是那一点燎原的星火，怎会有今天!建党节，献上真挚的祝福!",
		"党旗先烈血染红，党章生命来验证，党的光辉照万代，党的恩情人民记。七一建党节来到，祝愿大家身体好，听党指挥多努力，勤劳致富是正道!",
		"七一到，幸福党指示下达：要坚持快乐原则不动摇，一手抓财富，一手抓健康，切忌眉毛胡子一把抓!往“钱”看，往厚赚，幸福是你的。",
		"今天是党的生日，祝你前途似锦锐不可“党”，事业发展无可阻“党”，身体强壮以一“党”百，爱情美满甜蜜难“党”，快乐幸福风流倜“党”，祝党节快乐。",
		"跟着党走，吃喝玩乐啥都有，穿不愁，住不愁，努力工作向前走，困难面前有援手，坚强后盾固可守，党的生日来问候，鱼水情相守，美好在前，七一快乐!",
		"七一到了，我要给你建立好运根据地，帮你消灭霉运;巩固财富碉堡，帮你阻击贫穷;训练快乐部队，帮你击退忧愁。请接受我的命令，过快乐的建党日!",
		"此短信躲过了重重围追堵截，爬雪山，过草地，飞过泸定桥，横渡金沙江，登上南湖画舫，穿越延安窑洞。历经千辛，终于赶在七一送给你，祝你开心过七一!",
		"今天是七一建党日，借着节日的气息我要送上对你的祝福。祝愿你生活风调雨顺，爱情甜蜜无边，事业一帆风顺。总之，祝愿你一切都好!",
		"红旗迎风展，繁星闪一闪;党徽耀长空，梦想永不断;烟花多璀璨，豪情谱新篇;为党来高歌，祝福冲云端。七一建党节，共创美好新明天!",
		"建党节里歌颂党，党的领导放光芒，领导人民齐致富，创建和谐美名扬，一颗红心献给党，坚定信念跟党走，愿党领导创辉煌，国泰民安幸福长!",
		"七月党旗飘啊飘，画着斧头和镰刀。斧头劈开金银山，镰刀收获宝。压力烦恼一扫倒，吓得霉运溜溜跑。七一，愿你事业步步高。",
		"七月红旗迎风扬，党的生日到身旁。赤日炎炎光芒亮，党的恩情满心房。各族儿女齐欢畅，党的功绩万古芳。铭记党永不忘，沐浴党辉幸福长。建党节到了，愿党永远年轻。",
	}
	return list[tool.RandInt(0, len(list))]
}
