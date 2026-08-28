"""提交并推送本轮改动。"""

import os
import re
import subprocess

ROOT = r'e:\code\dnscat'
OUT = open(os.path.join(ROOT, 'zc.txt'), 'w', encoding='utf-8')

MSG = """feat(deploy): 一键安装脚本支持语言选择与二进制/Docker 双模式

安装脚本
- 启动先选语言（中/英），再选安装方式与角色；全部提示语走消息表，
  也可用 --lang/--mode/--role 非交互指定
- 支持 curl | bash 独立运行：管道执行时 BASH_SOURCE 不是真实文件，
  改为按需拉取源码快照（Docker 模式与源码编译才需要），纯二进制安装无需仓库
- 主控装完后打印填好令牌与地址的边缘端一键加入命令
- 架构支持扩展到 amd64/386/arm64/armv7
- 修复 set -e 陷阱：函数末句的 [[ ]] && 赋值在条件为假时会让函数返回非零，
  导致脚本静默终止（表现为退出码 1 且无任何输出）
- 53 端口处理加固：关闭 systemd-resolved stub 前先确定可用上游，
  按 resolved 文件 / resolvectl / 现有非环回地址三级回退，
  一个都找不到则中止安装，不把主机留在无法解析域名的状态
- 识别 DNSCat 自身占用 53 的情况并先停旧服务，否则重装会被自己挡住
- 配置目录属主给到服务账号并实测可读性

服务端
- Web 控制台资源改为可编译进二进制（构建标签 embedui）：
  systemd 的 WorkingDirectory 指向数据目录，靠相对路径读 web/dist 找不到文件，
  二进制安装的控制台因此完全加载不出来。默认构建不受影响
- SQLite 换为纯 Go 驱动 glebarez/sqlite：原 gorm.io/driver/sqlite 依赖
  mattn/go-sqlite3 是 CGO 实现，CGO_ENABLED=0 交叉编译出的产物运行时报
  requires cgo to work，四种发布架构全部不可用
- LoadConfig 不再静默忽略配置读取失败：权限不足时会明确报错，
  此前会悄悄回退到含相对路径数据库的默认值，报出与权限无关的误导性错误

CI
- release.yml：打 tag 构建 linux amd64/386/arm64/armv7 的 server/node/cli，
  生成校验和并上传 Release
- ci.yml：go build/vet/test、embedui 构建、脚本语法与 BOM/CRLF 检查、
  compose 配置校验
"""


def git(*args, check=True):
    r = subprocess.run(['git', '-C', ROOT] + list(args), capture_output=True,
                       text=True, encoding='utf-8', errors='replace', timeout=600)
    print('$ git %s' % ' '.join(args[:4]), file=OUT)
    if r.stdout.strip():
        print(r.stdout.strip()[:2000], file=OUT)
    err = r.stderr.strip()
    if err and 'LF will be replaced' not in err:
        print('[stderr] %s' % err[:1200], file=OUT)
    OUT.flush()
    if check and r.returncode != 0:
        print('失败 rc=%d' % r.returncode, file=OUT)
        OUT.close()
        raise SystemExit(1)
    return r


git('add', '-A')

r = git('diff', '--cached', '--name-status')
staged = []
for line in r.stdout.splitlines():
    if not line.strip():
        continue
    st, path = line.split('\t', 1)
    staged.append((st.strip(), path.strip()))

print('\n暂存 %d 项' % len(staged), file=OUT)

# 不该入库的东西
bad = [p for _s, p in staged
       if p.startswith(('zz-', 'bin/', 'web/dist/', '_'))
       or p.endswith(('.log', '.db'))
       or '部署云测试机' in p or '测试云机' in p or '.env' in p]
print('可疑暂存项: %s' % (bad if bad else '无'), file=OUT)
if bad:
    print('存在不该入库的项，已中止', file=OUT)
    OUT.close()
    raise SystemExit(1)

git('commit', '-m', MSG)
print('', file=OUT)
git('log', '--oneline', '-3')
print('', file=OUT)

# 推送
git('push', 'origin', 'master')
print('', file=OUT)
git('status', '--short')
r = git('log', '--oneline', '-2')
print('\n远端跟踪:', file=OUT)
git('rev-parse', '--abbrev-ref', 'HEAD@{upstream}', check=False)
git('log', '--oneline', 'origin/master', '-2', check=False)

OUT.close()
print('DONE')
