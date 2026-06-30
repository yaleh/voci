# PTT 热键：输入框聚焦时可用的语音激活热键调研

**日期**: 2026-06-30
**状态**: 调研完成，待实施

## 问题

当前 voci web 界面的 PTT（Push To Talk）热键是 `Space`，但在输入框聚焦时被显式排除（`recorder.js:513`，`e.target !== composeEl`），以防止打字时误触发录音。用户需要一个即使在输入框聚焦时也能激活语音输入的热键。

## 约束

1. **不能是 `Ctrl+Space`**——Windows 下用于切换输入法，冲突
2. **不能是 `Shift+Space`**——输入框中产生空格字符，无法区分
3. **不能是 `Alt+Space`**——多数 Linux DE 用它打开窗口菜单，冲突
4. **必须是 hold-to-talk 方式**——按下说话、松开结束，符合现有 PTT 交互模式
5. **跨平台兼容**——需在 Windows、macOS、Linux 浏览器中正常工作

## 行业调研

### 同类工具的热键选择

| 工具 | 平台 | 默认 PTT 热键 | 策略 |
|---|---|---|---|
| [Voicebox](https://github.com/jamiepine/voicebox) | macOS, Windows | macOS: `Right Cmd + Right Option`<br>Windows: `Right Ctrl + Right Shift` | 独占右侧修饰键，避开 AltGr 和左键组合 |
| [PipeVoice](https://github.com/Powleads/PipeVoice) | Windows | `Right Ctrl + Right Shift`（剪贴板热键）| 右侧修饰键组合 |
| [VoiceMacros](https://www.mediachance.com/voicemacros/) | Windows | `Right Ctrl` 或 `Right Shift`（单键）| 最流行选择：单右侧修饰键 |
| [VoiceCode](https://www.npmjs.com/package/voicecode) | 跨平台 / 浏览器 | `AltRight`（右侧 Alt）| 单右侧修饰键 |
| [whisper-input](https://pypi.org/project/whisper-input/) | Linux, macOS | Linux: `Right Ctrl`<br>macOS: `Right Cmd` | 区分左右修饰键 |
| [faster-whisper-hotkey](https://github.com/blakkd/faster-whisper-hotkey) | Linux, macOS, Windows | `Pause` / `F4` / `F8` | 功能键方案（不使用修饰键）|

### 为什么行业一致选择右侧修饰键

1. **左手修饰键冲突高**——Ctrl+C/V/Z、Ctrl+Space 切输入法、Alt+Tab 切窗口……几乎无法找到空闲的左手组合
2. **右侧修饰键几乎不被应用使用**——正常用户极少用 Right Ctrl / Right Shift
3. **按住修饰键不触发 key-repeat**——天然适配 hold-to-talk 模式
4. **AltGr 安全**——国际键盘（德语/法语/西班牙语）使用 Right Alt 作为 AltGr 来输入特殊字符，使用 Right Ctrl 可以避开这个冲突
5. **在输入框中不产生字符**——修饰键本身不会在 textarea/input 中插入任何内容

### 浏览器中区分左右修饰键

`KeyboardEvent` 提供 `.location` 属性区分左右键：

| 属性 | 值 | 含义 |
|---|---|---|
| `e.code` | `ControlRight` / `ControlLeft` | 明确的键码 |
| `e.location` | `1` (DOM_KEY_LOCATION_LEFT) | 左侧 |
| `e.location` | `2` (DOM_KEY_LOCATION_RIGHT) | 右侧 |

所有现代浏览器均支持。兼容性：Chrome 37+, Firefox 31+, Safari 10.1+, Edge 79+。

## 建议方案：`Right Ctrl`（按住说话）

### 方案描述

在现有 `Space` PTT 之外，新增 `Right Ctrl` 作为第二热键：

- **Space**：当前行为不变——`target != composeEl` 时触发 PTT
- **Right Ctrl**：新增——始终触发 PTT，无视 `target`，输入框聚焦时也能用

两个热键共存，互不干扰。

### 评估

| 维度 | 评价 |
|---|---|
| **输入法冲突** | ✅ 无——切输入法用的是 `Left Ctrl + Space`，Right Ctrl 不参与 |
| **输入框内打字** | ✅ 无——修饰键不产生字符 |
| **人体工学** | ✅ 右手小指按住 Right Ctrl，左手继续打字 |
| **心智模型** | ✅ "Space = 说话，Right Ctrl = 打字时也能说话" |
| **浏览器兼容** | ✅ `KeyboardEvent.location` 全平台支持 |
| **行业验证** | ✅ VoiceMacros、PipeVoice、whisper-input 的默认选择 |

### 备选

如果单键 `Right Ctrl` 有顾虑，可采用 **`Right Ctrl + Right Shift`** 组合键（Voicebox/PipeVoice 默认），进一步降低误触概率，但需用户同时按住两个键，人体工学稍差。

### 实现位置

`internal/daemon/web/recorder.js:512-522`——在现有 Space keydown/keyup 监听器中新增 `ControlRight` 分支。

## 参考

- [Voicebox v0.5.0 — right-Cmd + right-Option / right-Ctrl + right-Shift defaults](https://github.com/jamiepine/voicebox)
- [PipeVoice — right-Ctrl + right-Shift hotkey](https://github.com/Powleads/PipeVoice)
- [VoiceMacros FAQ — Right Ctrl or Right Shift most popular](https://www.mediachance.com/voicemacros/faq.html)
- [whisper-input — right-Ctrl / right-Cmd](https://pypi.org/project/whisper-input/)
- [MDN: KeyboardEvent.location](https://developer.mozilla.org/en-US/docs/Web/API/KeyboardEvent/location)
