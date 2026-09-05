package utils

//所有链接按需取用，用webVPN就注释校园网内的，用校园网内的就注销webVPN的

// 教务管理系统 下面那个是校园网内的
//var Jwc_url string = "https://https-authserver-csuft-edu-cn-443.webvpn.csuft.edu.cn/authserver/login?service=https%3A%2F%2Fhttp-jwxt-csuft-edu-cn-80.webvpn.csuft.edu.cn%2F"

var Jwc_url string = "https://authserver.csuft.edu.cn/authserver/login?service=http%3A%2F%2Fjwxt.csuft.edu.cn%2F"

//下面那个是webvpn首页
//var Jwc_url string = "https://https-authserver-csuft-edu-cn-443.webvpn.csuft.edu.cn/authserver/login?service=https://webvpn.csuft.edu.cn/callback/cas/eAF0IG5N"

// 验证码
var Captcha_url string = "https://https-authserver-csuft-edu-cn-443.webvpn.csuft.edu.cn/authserver/needCaptcha.html?"

// 直接获取验证码
var Captcha_direct_url = ""

// 教评
var Evaluation_url string = "https://https-jxzlpt-csuft-edu-cn-443.webvpn.csuft.edu.cn/api/manage/cas/toUrl?type=pc"

// 查成绩 下面那个是校园网内的
//var Grade_url string = "https://http-jwxt-csuft-edu-cn-80.webvpn.csuft.edu.cn/jsxsd/kscj/cjcx_list"

var Grade_url = "http://jwxt.csuft.edu.cn/jsxsd/kscj/cjcx_list"

// 校园网内用上面那个
var Grade_level_url string = "http://jwxt.csuft.edu.cn/jsxsd/kscj/djkscj_list"

//var Grade_level_url string = "https://http-jwxt-csuft-edu-cn-80.webvpn.csuft.edu.cn/jsxsd/kscj/djkscj_list"

// 查课表 下面那个是校园网内的
// var Course_url = "https://http-jwxt-csuft-edu-cn-80.webvpn.csuft.edu.cn/jsxsd/xskb/xskb_list.do?viweType=0"
var Course_url string = "http://jwxt.csuft.edu.cn/jsxsd/xskb/xskb_list.do?viweType=0"

// 查考试安排 下面的是校园网内的
var Exam_url = "https://http-jwxt-csuft-edu-cn-80.webvpn.csuft.edu.cn/jsxsd/xsks/xsksap_list"

//var Exam_url = "http://jwxt.csuft.edu.cn/jsxsd/xsks/xsksap_list"

// 查电费 电费系统也是个几把，超级性能超级流畅响应超级快
var Electric_url string = ""
