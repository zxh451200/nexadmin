// 官网客服挂件增强层：不改动 chat-widget.js 的既有会话逻辑，只通过 DOM 事件与
// 观察器补上商品引用、翻译、名牌/地址卡片、客服身份/本地时间、表情与更多菜单。样式随脚本注入，
// 四套主题共用一份；无脚本时原有挂件照常工作。
(function () {
  'use strict';

  var CSS = [
    '.chat-panel{position:relative}',
    '.chat-product{display:flex;align-items:center;gap:8px;margin:0 10px 8px;padding:8px;border:1px solid rgba(128,128,128,.28);border-radius:4px;background:Canvas;color:CanvasText;font-size:11px}',
    '.chat-product[hidden]{display:none}',
    '.chat-product-cover{flex:none;width:38px;height:38px;overflow:hidden;border-radius:3px;background:rgba(128,128,128,.12)}',
    '.chat-product-cover img{width:100%;height:100%;object-fit:cover}',
    '.chat-product-info{display:flex;flex:1;flex-direction:column;gap:2px;min-width:0}',
    '.chat-product-info strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px}',
    '.chat-product-info span{opacity:.75}',
    '.chat-product-remove{flex:none;border:0;background:transparent;color:inherit;font-size:18px;line-height:1;cursor:pointer;opacity:.6}',
    '.chat-product-remove:hover{opacity:1}',
    // 客服名 + 本地时间跟标题同一行（对齐阿里商详的头部）：中间用 · 分隔，过长省略
    '.chat-agent{font-size:11px;opacity:.72;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}',
    '.chat-agent>span+span::before{content:"·";margin-inline:6px;opacity:.6}',
    '.chat-agent>span:empty{display:none}',
    // 一行放不下时只让「姓名 · 时间」收窄省略，标题和按钮不参与压缩，避免整行被挤到换行
    '.chat-identity-text{flex-wrap:nowrap;overflow:hidden}',
    '.chat-identity-text>strong{flex:none;white-space:nowrap}',
    '.chat-agent{min-width:0}',
    '.chat-agent[hidden]{display:none}',
    '.chat-assurance{margin:0;padding:6px 12px;border-block-end:1px solid rgba(128,128,128,.18);background:rgba(128,128,128,.06);font-size:10.5px;line-height:1.5;opacity:.8}',
    // 工具栏/头部的小按钮统一成「无边框 + 圆角 + hover 底色」：主题里 .chat-attach 是 32px 白底方框，
    // 和旁边 28px 无框的表情/翻译并排就成了三种规格，一排图标看着高低不齐。
    '.chat-tool,.chat-attach{display:inline-flex;align-items:center;justify-content:center;width:30px;height:30px;flex:none;padding:0;border:0;border-radius:8px;background:transparent;color:inherit;cursor:pointer;opacity:.75}',
    '.chat-tool svg,.chat-attach svg{width:17px;height:17px;flex:none}',
    '.chat-tool:hover,.chat-attach:hover{opacity:1;background:rgba(128,128,128,.16);color:inherit}',
    // 头部是深色底，灰底会糊成一块浅斑，改用白色高光
    '.chat-head .chat-tool:hover{background:rgba(255,255,255,.14)}',
    '.chat-tool[disabled],.chat-attach[disabled]{opacity:.4;cursor:default}',
    '.chat-send svg{width:17px;height:17px}',
    // 头部「放大/还原」占同一个按钮位，两态只换图标；替掉原来的 ··· 菜单——
    // 那个菜单只有「放大窗口 / 清空输入」两项，一个动作属于窗口、一个属于输入框，
    // 都挂在右上角既不贴身又要点两次（清空还是不可逆操作，混在中性菜单里没有区分）。
    '.chat-expand>span{display:none;align-items:center;justify-content:center}',
    '.chat-expand>.chat-ico-expand{display:inline-flex}',
    '.chat.is-expanded .chat-expand>.chat-ico-expand{display:none}',
    '.chat.is-expanded .chat-expand>.chat-ico-restore{display:inline-flex}',
    // 清空输入只在真有草稿时露头：用 :placeholder-shown 判断，发送后 chat-widget.js
    // 直接改 value（不触发 input 事件）也会自动收起，不用再同步一份状态。
    '.chat-clear{display:none}',
    '.chat-compose textarea:not(:placeholder-shown)~.chat-compose-bar .chat-clear{display:inline-flex}',
    // 按钮是凭空多出来的，不能让整排跳一下：把它右侧的提示文字改成吃掉剩余空间的弹性项，
    // 并把发送键的 auto 边距归零（否则两个 auto 边距各分一半，清空键又飘回中间）。
    '.chat-compose textarea:not(:placeholder-shown)~.chat-compose-bar .chat-hint{margin-inline-start:0;flex:1 1 auto;min-width:0}',
    '.chat-compose textarea:not(:placeholder-shown)~.chat-compose-bar .chat-send{margin-inline-start:0}',
    '.chat-clear:hover{background:rgba(235,87,87,.14);color:var(--red,#eb5757)}',
    '.chat-emoji-pop{position:absolute;z-index:20;display:flex;flex-wrap:wrap;gap:4px;padding:8px;border:1px solid rgba(128,128,128,.3);border-radius:4px;background:Canvas;color:CanvasText;box-shadow:0 12px 28px rgba(0,0,0,.18)}',
    '.chat-emoji-pop[hidden]{display:none}',
    '.chat-emoji-pop{inset-inline-start:8px;inset-block-end:96px;display:grid;grid-template-columns:repeat(9,minmax(0,1fr));gap:2px;box-sizing:border-box;width:min(304px,calc(100% - 16px));max-height:216px;overflow-y:auto;padding:8px}',
    // content-visibility：176 个 emoji 首开时光栅化全部字形，弱机/高负载下点开有可感知的停顿；
    // 视口内只有 ~45 个，屏外的跳过绘制（滚动到再画），首开绘制量直接砍到 1/4
    '.chat-emoji-pop button{display:flex;align-items:center;justify-content:center;width:100%;aspect-ratio:1/1;padding:0;border:0;border-radius:4px;background:transparent;color:inherit;font-size:18px;line-height:1;cursor:pointer;content-visibility:auto;contain-intrinsic-size:auto 32px}',
    '.chat-emoji-pop button:hover{background:rgba(128,128,128,.18)}',
    '.chat-emoji-pop::-webkit-scrollbar{width:6px}',
    '.chat-emoji-pop::-webkit-scrollbar-thumb{border-radius:3px;background:rgba(128,128,128,.4)}',
    // 译文链接并进「名字 · 时间」那一行：单独一行会让每条客服消息多占 16px，
    // 一条会话里十几条就是半屏空白。
    '.chat-msg-translate{margin:0;padding:0;border:0;background:transparent;color:inherit;font:inherit;font-size:inherit;text-decoration:underline;text-underline-offset:2px;cursor:pointer;opacity:.75}',
    '.chat-msg-translate:hover{opacity:1}',
    '.chat-meta-sep{opacity:.6}',
    // 翻译设置弹层：对齐阿里聊天窗口的「翻译设置」——收到/发送两组开关各带语言选择。
    // 位置挂在头部下沿（--chat-head-h 由脚本实时量），配色沿用 Canvas/currentColor。
    '.chat-settings-pop{position:absolute;z-index:22;inset-inline-end:10px;inset-block-start:calc(var(--chat-head-h,86px) + 4px);box-sizing:border-box;width:min(320px,calc(100% - 16px));padding:14px;border:1px solid rgba(128,128,128,.28);border-radius:8px;background:Canvas;color:CanvasText;box-shadow:0 16px 38px rgba(0,0,0,.22);font-size:12px}',
    '.chat-settings-pop[hidden]{display:none}',
    '.chat-settings-pop h3{margin:0 0 10px;font-size:13.5px;font-weight:600}',
    '.chat-settings-row{display:flex;align-items:center;justify-content:space-between;gap:8px}',
    '.chat-settings-row strong{font-size:12.5px;font-weight:600}',
    // 开关用原生 checkbox 拟态：键盘可达，无需脚本画状态；选中色随主题变量
    '.chat-switch{position:relative;width:36px;height:20px;flex:none;appearance:none;-webkit-appearance:none;margin:0;border:1px solid rgba(128,128,128,.4);border-radius:10px;background:rgba(128,128,128,.22);cursor:pointer;transition:background .15s ease,border-color .15s ease}',
    '.chat-switch::after{content:"";position:absolute;top:2px;inset-inline-start:2px;width:14px;height:14px;border-radius:50%;background:Canvas;box-shadow:0 1px 2px rgba(0,0,0,.25);transition:inset-inline-start .15s ease}',
    '.chat-switch:checked{background:var(--pc-accent,CanvasText);border-color:transparent}',
    '.chat-switch:checked::after{inset-inline-start:18px}',
    '.chat-settings-fields{display:flex;align-items:center;gap:8px;margin:9px 0 2px}',
    '.chat-settings-fields span{flex:none;opacity:.72}',
    // 语言下拉复用地址卡的自定义控件：原生 <select> 的展开层由系统绘制，系统蓝高亮和浮层配色对不上
    '.chat-settings-pop .chat-card-select{flex:1;min-width:0}',
    '.chat-settings-pop .chat-card-trigger{height:30px;font-size:12.5px}',
    '.chat-settings-pop .chat-card-trig-flag{display:none}',
    '.chat-settings-hint{margin:6px 0 13px;font-size:11px;line-height:1.5;opacity:.6}',
    '.chat-settings-actions{display:flex;justify-content:flex-end;margin-top:2px}',
    '.chat-settings-actions button{height:32px;padding:0 14px;border:1px solid rgba(128,128,128,.36);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:12.5px;cursor:pointer}',
    '.chat-settings-actions button:hover{border-color:currentColor}',
    '.chat-settings-actions .is-primary{border-color:transparent;background:var(--pc-accent,CanvasText);color:Canvas;font-weight:600}',
    '.chat-settings-actions .is-primary:hover{filter:brightness(1.08);border-color:transparent}',
    // 翻译快捷浮层（工具栏文A 按钮）：立即翻译主按钮 + 两个自动翻译开关行 + 设置入口。
    // 贴着工具栏弹出（与表情、地址卡同位置）：原来从头部下方展开，离触发按钮半屏远，
    // 还压住最新一条消息。宽度比完整表单窄一档，弱化「设置表单」的观感。
    '.chat-settings-pop.is-quick{inset-block-start:auto;inset-block-end:96px;inset-inline-end:8px;width:min(300px,calc(100% - 16px));padding:12px 14px 10px}',
    '.chat-settings-pop:focus{outline:none}',
    '.chat-tr-head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:8px}',
    '.chat-tr-head h3{margin:0;font-size:12.5px}',
    '.chat-tr-now{box-sizing:border-box;width:100%;height:32px;margin-bottom:4px;border:1px solid transparent;border-radius:4px;background:var(--pc-accent,CanvasText);color:Canvas;font:inherit;font-size:12.5px;font-weight:600;cursor:pointer}',
    '.chat-tr-now:hover{filter:brightness(1.08)}',
    '.chat-tr-now[disabled]{opacity:.5;cursor:default;filter:none}',
    '.chat-tr-row{display:flex;align-items:center;gap:8px;padding:8px 2px;font-size:12px;cursor:pointer}',
    '.chat-tr-row+.chat-tr-row{border-top:1px solid rgba(128,128,128,.14)}',
    '.chat-tr-row .chat-switch{flex:none}',
    '.chat-tr-more{margin-top:2px;padding:0;border:0;background:transparent;color:inherit;font:inherit;font-size:11.5px;text-decoration:underline;text-underline-offset:3px;cursor:pointer;opacity:.7}',
    '.chat-tr-more:hover{opacity:1}',
    '.chat-tr-head .chat-tr-more{margin:0;text-decoration:none}',
    '.chat-tr-head .chat-tr-more:hover{text-decoration:underline}',
    '.chat-tr-msg{margin:6px 2px 2px;font-size:11.5px;line-height:1.5;color:inherit;opacity:.7}',
    '.chat-tr-msg.is-err{color:var(--red,#eb5757);opacity:1}',
    '.chat-compose textarea{resize:vertical;max-height:220px}',
    '.chat-card-pop{position:absolute;z-index:22;inset-inline-start:8px;inset-block-end:96px;box-sizing:border-box;width:min(324px,calc(100% - 16px));padding:16px 16px 14px;border:1px solid rgba(128,128,128,.28);border-radius:8px;background:Canvas;color:CanvasText;box-shadow:0 16px 38px rgba(0,0,0,.22);font-size:12px}',
    '.chat-card-pop[hidden]{display:none}',
    // 展开面板（760px）里的地址卡放大一档：380px 收起面板装不下大表单，
    // 打开卡片时 JS 会先把面板切到展开态，这里给足字号和控件尺寸的对齐增量。
    '.chat.is-expanded .chat-card-pop{width:min(480px,calc(100% - 32px));inset-inline-start:16px;padding:20px 22px 16px;font-size:13px}',
    '.chat.is-expanded .chat-card-pop h3{font-size:15px}',
    '.chat.is-expanded .chat-card-hint{margin-bottom:18px;font-size:12px}',
    '.chat.is-expanded .chat-card-error{font-size:12px}',
    '.chat.is-expanded .chat-card-fields{gap:14px}',
    '.chat.is-expanded .chat-card-fields label{gap:6px;font-size:12.5px}',
    '.chat.is-expanded .chat-card-fields input,.chat.is-expanded .chat-card-trigger,.chat.is-expanded .chat-phone-wrap{height:38px;font-size:13.5px}',
    '.chat.is-expanded .chat-card-actions button{height:40px;font-size:13.5px}',
    // 翻译设置里的语言下拉不要跟着地址卡放大：这套表单是 12.5px 的密度
    '.chat.is-expanded .chat-settings-pop .chat-card-trigger{height:30px;font-size:12.5px}',
    '.chat.is-expanded .chat-settings-pop .chat-card-options button{font-size:12px;line-height:30px}',
    '.chat.is-expanded .chat-card-search{height:32px;font-size:12.5px}',
    '.chat.is-expanded .chat-card-options button{font-size:12.5px;line-height:32px}',
    '.chat.is-expanded .chat-phone-dial{font-size:13px}',
    '.chat-card-pop h3{margin:0 0 4px;font-size:13.5px;font-weight:600}',
    '.chat-card-hint{margin:0 0 16px;font-size:11px;line-height:1.5;opacity:.66}',
    '.chat-card-fields{display:grid;grid-template-columns:1fr 1fr;gap:12px 10px}',
    // 不透明度只能落在字段文字上：label 整体 opacity 会把里面的下拉展开层
    // 一起压成 82% 半透明，底下的「详细地址」就透出来了（opacity 造出的是整组合成）
    '.chat-card-fields label{display:flex;flex-direction:column;gap:5px;min-width:0;font-size:11.5px}',
    // 星号单独一层：放在半透明的标签文字里会被一起压成粉红
    '.chat-card-label{display:flex;align-items:center;gap:2px}',
    '.chat-card-fields .chat-card-label>span{opacity:.82}',
    '.chat-card-fields .req{font-style:normal;color:var(--red,#eb5757)}',
    '.chat-card-fields label.wide{grid-column:1/-1}',
    '.chat-card-fields input{box-sizing:border-box;width:100%;height:32px;padding:0 10px;border:1px solid rgba(128,128,128,.34);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:12.5px;transition:border-color .15s ease,box-shadow .15s ease}',
    // 聚焦别用深色描边：currentColor 在浅色主题里就是近黑，1px 边框叠 3px 光晕后
    // 比卡片本身还抢眼。改成「边线加深一档 + 一层极淡的光晕」，安静但看得见。
    '.chat-card-fields input:focus{outline:none;border-color:rgba(128,128,128,.72);box-shadow:0 0 0 3px rgba(128,128,128,.13);background:rgba(128,128,128,.03)}',
    // 校验失败标在可见控件上：下拉的取值位是隐藏 input，红框要落到触发钮
    '.chat-card-fields input.is-invalid,.chat-card-fields .chat-card-trigger.is-invalid{border-color:rgba(235,87,87,.72);box-shadow:0 0 0 3px rgba(235,87,87,.12)}',
    '.chat-card-error{margin:10px 0 0;font-size:11.5px;line-height:1.5;color:var(--red,#eb5757)}',
    '.chat-card-actions{display:flex;gap:10px;margin-top:18px}',
    '.chat-card-actions button{flex:1;height:34px;border:1px solid rgba(128,128,128,.36);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:12.5px;cursor:pointer}',
    '.chat-card-actions button:hover{border-color:currentColor}',
    // 主按钮做反色实心：原来主次都只有一圈边框，只靠字重区分，一眼看不出该点哪个。
    // 注意不能写 background:currentColor 再改 color —— 同一规则里 color 先落值，
    // currentColor 会跟着变成 Canvas，按钮就白底白字消失了；用 CanvasText 显式取反色。
    '.chat-card-actions .is-primary{border-color:transparent;background:var(--pc-accent,CanvasText);color:Canvas;font-weight:600}',
    '.chat-card-actions .is-primary:hover{filter:brightness(1.08)}',
    // 地址卡片的国家/州省下拉：触发钮长得和输入框一致，展开层浮在表单上面；
    // 跟 .chat-card-pop 一样用 Canvas/currentColor，浅深主题都不用另配色。
    '.chat-card-select{position:relative}',
    '.chat-card-trigger{display:flex;align-items:center;gap:5px;box-sizing:border-box;width:100%;height:32px;padding:0 10px;border:1px solid rgba(128,128,128,.34);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:12.5px;cursor:pointer;text-align:start;transition:border-color .15s ease,box-shadow .15s ease}',
    '.chat-card-trigger:hover,.chat-card-trigger:focus-visible{border-color:rgba(128,128,128,.72)}',
    '.chat-card-trig-flag{flex:none;font-size:13px;line-height:1}',
    '.chat-card-trig-flag:empty{display:none}',
    '.chat-card-trig-txt{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}',
    '.chat-card-trig-txt.is-ph{opacity:.5}',
    '.chat-card-trig-chev{flex:none;display:inline-flex;color:inherit;opacity:.55}',
    '.chat-card-trig-chev svg{width:14px;height:14px}',
    '.chat-card-drop{position:absolute;z-index:26;top:100%;inset-inline:0;margin-top:4px;border:1px solid rgba(128,128,128,.34);border-radius:6px;background:Canvas;color:CanvasText;box-shadow:0 10px 26px rgba(0,0,0,.2);overflow:hidden}',
    '.chat-card-drop[hidden]{display:none}',
    '.chat-card-search{box-sizing:border-box;width:100%;height:28px;padding:0 9px;border:0;border-block-end:1px solid rgba(128,128,128,.24);background:transparent;color:inherit;font:inherit;font-size:12px}',
    '.chat-card-search:focus{outline:none;background:rgba(128,128,128,.05)}',
    '.chat-card-options{max-height:188px;overflow-y:auto}',
    '.chat-card-options::-webkit-scrollbar{width:6px}',
    '.chat-card-options::-webkit-scrollbar-thumb{border-radius:3px;background:rgba(128,128,128,.4)}',
    '.chat-card-options button{display:flex;align-items:center;gap:6px;width:100%;padding:0 9px;border:0;background:transparent;color:inherit;font:inherit;font-size:12px;line-height:28px;cursor:pointer;text-align:start}',
    '.chat-card-options button:hover{background:rgba(128,128,128,.14)}',
    '.chat-card-options button.is-sel{font-weight:600}',
    '.chat-opt-flag{flex:none;line-height:1}',
    '.chat-opt-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}',
    '.chat-opt-dial{flex:none;opacity:.55;font-size:11px}',
    '.chat-opt-empty{display:block;padding:8px 9px;font-size:11.5px;opacity:.55}',
    // 电话区号前缀：随国家联动，纯展示；提交值里才真正拼上区号
    '.chat-phone-wrap{display:flex;align-items:center;box-sizing:border-box;width:100%;height:32px;padding:0 10px;border:1px solid rgba(128,128,128,.34);border-radius:4px;background:transparent;transition:border-color .15s ease,box-shadow .15s ease}',
    '.chat-phone-wrap:focus-within{border-color:rgba(128,128,128,.72);box-shadow:0 0 0 3px rgba(128,128,128,.13);background:rgba(128,128,128,.03)}',
    '.chat-card-fields .chat-phone-wrap input{height:100%;flex:1;min-width:0;border:0;padding:0;background:transparent;font-size:12.5px}',
    '.chat-card-fields .chat-phone-wrap input:focus{outline:none;border:0;box-shadow:none;background:transparent}',
    '.chat-phone-dial{flex:none;padding-inline-end:7px;margin-inline-end:7px;border-inline-end:1px solid rgba(128,128,128,.3);font-size:12px;opacity:.72}',
    '.chat-phone-dial[hidden]{display:none}',
    '.chat-bubble-card{white-space:pre-wrap;border-inline-start:2px solid currentColor;font-size:11.5px;line-height:1.6}',
    '.chat.is-expanded .chat-panel{width:min(760px,calc(100vw - 40px));max-width:calc(100vw - 40px);height:min(86dvh,840px);max-height:86dvh}',
    '.chat-panel{transition:width .2s ease,height .2s ease}',
    '@media(max-width:760px){.chat.is-expanded .chat-panel{width:calc(100vw - 16px);max-width:calc(100vw - 16px);height:min(88dvh,760px)}}',
    '@media(prefers-reduced-motion:reduce){.chat-panel{transition:none}}'
  ].join('');

  // 表情按「笑脸 → 手势 → 商务物件」排布，对齐参考面板的视觉顺序；
  // 用系统 emoji 而非平台自有图片：自托管、无外部请求、跨主题不变色。
  var EMOJI_FACES = ['😀','😃','😄','😁','😆','😅','🤣','😂','🙂','🙃','😉','😊','😇','🥰','😍','🤩','😘','😗','😚','😙','😋','😛','😜','🤪','😝','🤑','🤗','🤭','🤫','🤔','🤨','😐','😑','😶','😏','😒','🙄','😬','😌','😔','😪','🤤','😴','😷','🤒','🤕','🥵','🥶','😵','🤯','🤠','🥳','😎','🤓','🧐','😕','🙁','😮','😯','😲','😳','🥺','😦','😧','😨','😰','😥','😢','😭','😱','😖','😣','😞','😓','😩','😫','🥱','😤','😡','😠','🤬','😈','👻','🤖','😺','😸','😹','😻','😼','😽','🙀','😿','😾'];
  var EMOJI_HANDS = ['👋','🤚','🖐','✋','🖖','👌','🤌','🤏','✌️','🤞','🤟','🤘','🤙','👈','👉','👆','👇','☝️','👍','👎','✊','👊','🤛','🤜','👏','🙌','👐','🤲','🤝','🙏','💪','🦾','✍️','💅'];
  var EMOJI_OBJECTS = ['❤️','🧡','💛','💚','💙','💜','💔','💕','💖','💗','🌹','🌷','💐','🎁','🎉','🎊','✉️','📧','📦','📮','⏰','⏳','✈️','🚢','🚚','📍','🔥','💰','💵','💳','📈','📉','✅','❌','⭐','🌟','💡','🔔','📞','📱','🖥️','🏭','🏢','📋','📎','🔒','🔑','🛒','🤝'];
  var EMOJIS = EMOJI_FACES.concat(EMOJI_HANDS, EMOJI_OBJECTS);

  // 名牌 / 地址卡片图标与站上其他图标同源 Tabler Icons v3.36.0 outline（MIT）：
  // id / map-pin。公开站是服务端模板 + 原生 JS，这里只固化这两个 path 与统一的 stroke 口径，
  // 不再混入 @ant-design/icons 的实心图标，避免同一页面出现两套画风。
  var CARD_ICON_ATTRS = ' viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"';
  var CARD_ICONS = {
    name: '<svg' + CARD_ICON_ATTRS + '><path d="M3 4m0 3a3 3 0 0 1 3 -3h12a3 3 0 0 1 3 3v10a3 3 0 0 1 -3 3h-12a3 3 0 0 1 -3 -3z" /><path d="M9 10m-2 0a2 2 0 1 0 4 0a2 2 0 1 0 -4 0" /><path d="M15 8l2 0" /><path d="M15 12l2 0" /><path d="M7 16l10 0" /></svg>',
    address: '<svg' + CARD_ICON_ATTRS + '><path d="M9 11a3 3 0 1 0 6 0a3 3 0 0 0 -6 0" /><path d="M17.657 16.657l-4.243 4.243a2 2 0 0 1 -2.827 0l-4.244 -4.243a8 8 0 1 1 11.314 0z" /></svg>',
    // 下拉触发钮右端的 chevron-down，与卡片图标同一套 Tabler 口径
    chevron: '<svg' + CARD_ICON_ATTRS + '><path d="M6 9l6 6 6-6" /></svg>'
  };

  // 地址卡片的国家/州省数据集（plugins/website/static/country-region.json）：
  // 由 country-region-data（MIT）+ country-codes-list（MIT）+ i18n-iso-countries 中文国名（MIT）
  // 合并生成，来源与许可见 docs/功能与权限清单.md。首次打开地址卡才拉取，失败时退回手写输入。
  var COUNTRY_DATA = null;
  var COUNTRY_REQ = null;
  function fetchCountries() {
    return fetch('/website-assets/country-region.json', { credentials: 'same-origin' })
      .then(function (res) { if (!res.ok) throw new Error('HTTP ' + res.status); return res.json(); })
      .then(function (list) { COUNTRY_DATA = Array.isArray(list) ? list : []; return COUNTRY_DATA; });
  }
  function loadCountries() {
    if (COUNTRY_DATA) return Promise.resolve(COUNTRY_DATA);
    if (!COUNTRY_REQ) {
      COUNTRY_REQ = fetchCountries().catch(function (err) { COUNTRY_REQ = null; throw err; });
    }
    // 只在真拿不到时才退回手写输入：重部署/切网瞬间的一次失败不该把下拉直接降级，
    // 隔一拍重试一次再放弃；放弃时留一条 warn，下次有人问「怎么变成输入框了」能直接看到原因。
    return COUNTRY_REQ.catch(function (err) {
      return new Promise(function (resolve) { setTimeout(resolve, 600); })
        .then(fetchCountries)
        .catch(function (err2) {
          console.warn('[chat-enhance] 国家/地区数据不可用（/website-assets/country-region.json），已退回手写输入：', err2 || err);
          throw err2 || err;
        });
    });
  }
  // 国旗不必存数据：ISO2 码的两个字母各加 regional-indicator 偏移就是 emoji
  function flagEmoji(code) {
    var out = '';
    for (var i = 0; i < code.length; i++) out += String.fromCodePoint(0x1F1E6 + code.charCodeAt(i) - 65);
    return out;
  }
  // 站点语言是英文就显示英文国名（州省名本来就是英文，与阿里中文界面下的表现一致）
  function siteLangEn() {
    return /^en/i.test(String(document.documentElement.lang || ''));
  }

  // 城市级联数据（static/cities/<ISO2>.json，GeoNames CC BY 4.0，来源见功能与权限清单）：
  // 国家 → 州省 → 城市 三级。城市按国家拆文件按需拉取；404 = 该国没有城市数据，
  // 返回 null，城市字段退回手写输入。
  var CITY_CACHE = {};
  var CITY_REQ = {};
  function loadCities(code) {
    if (code in CITY_CACHE) return Promise.resolve(CITY_CACHE[code]);
    if (!CITY_REQ[code]) {
      CITY_REQ[code] = fetch('/website-assets/cities/' + code + '.json', { credentials: 'same-origin' })
        .then(function (res) {
          if (!res.ok) throw new Error('unavailable');
          return res.json();
        })
        .then(function (data) { CITY_CACHE[code] = data && typeof data === 'object' ? data : null; return CITY_CACHE[code]; })
        .catch(function () { CITY_CACHE[code] = null; CITY_REQ[code] = null; return null; });
    }
    return CITY_REQ[code];
  }

  function readI18n() {
    try {
      var el = document.getElementById('site-i18n');
      if (el) return JSON.parse(el.textContent || '{}');
    } catch (e) { /* ignore */ }
    return {};
  }

  function init() {
    var root = document.querySelector('[data-chat]');
    if (!root) return;
    if (!document.getElementById('site-chat-enhance-style')) {
      var st = document.createElement('style');
      st.id = 'site-chat-enhance-style';
      st.textContent = CSS;
      document.head.appendChild(st);
    }
    var i18n = readI18n();
    function t(k, fb) { return i18n[k] || fb; }

    var input = root.querySelector('[data-chat-input]');
    var form = root.querySelector('[data-chat-form]');
    var log = root.querySelector('[data-chat-log]');
    var panel = root.querySelector('[data-chat-panel]');
    var productBox = root.querySelector('[data-chat-product]');
    var productCover = root.querySelector('[data-chat-product-cover]');
    var productTitle = root.querySelector('[data-chat-product-title]');
    var productPrice = root.querySelector('[data-chat-product-price]');
    var productMoq = root.querySelector('[data-chat-product-moq]');
    var productRemove = root.querySelector('[data-chat-product-remove]');
    var translateBtn = root.querySelector('[data-chat-translate]');
    var emojiBtn = root.querySelector('[data-chat-emoji]');
    var emojiPop = root.querySelector('[data-chat-emoji-pop]');
    var expandBtn = root.querySelector('[data-chat-expand]');
    var clearBtn = root.querySelector('[data-chat-clear]');
    var agentBox = root.querySelector('[data-chat-agent]');
    var agentName = root.querySelector('[data-chat-agent-name]');
    var agentTime = root.querySelector('[data-chat-agent-time]');
    var chatTitle = root.querySelector('#chat-title');

    var state = { product: null, sent: false, timezone: '', expanded: false, outDone: false, trFailUntil: 0 };

    // ---- 翻译：设置（阿里式「翻译设置」）+ 批量客户端 + 本地缓存 ----
    // 站点语言清单由页面 #site-langs 下发（Go 侧只认清单内的语言码），前端不自带名单。
    var LANGS = (function () {
      try {
        var el = document.getElementById('site-langs');
        var list = el ? JSON.parse(el.textContent || '[]') : [];
        return Array.isArray(list) ? list : [];
      } catch (e) { return []; }
    })();
    var pageLang = String(document.documentElement.lang || '');
    if (!pageLang && LANGS.length) pageLang = LANGS[0].c;
    var siteDefaultLang = '';
    for (var li = 0; li < LANGS.length; li++) {
      if (LANGS[li].d) { siteDefaultLang = LANGS[li].c; break; }
    }
    if (!siteDefaultLang && LANGS.length) siteDefaultLang = LANGS[0].c;

    // 翻译设置持久化在访客侧 localStorage：autoIn/inTo 对应「收到的消息」，
    // autoOut/outFrom/outTo 对应「发送的消息」，空值回退页面语言与站点默认语言。
    var trSettings = (function () {
      var s = {};
      try { s = JSON.parse(localStorage.getItem('nexchat.translate') || '{}') || {}; } catch (e) { /* 隐私模式忽略 */ }
      return {
        autoIn: !!s.autoIn,
        inTo: String(s.inTo || ''),
        autoOut: !!s.autoOut,
        outFrom: String(s.outFrom || ''),
        outTo: String(s.outTo || '')
      };
    })();
    function saveTrSettings() {
      try { localStorage.setItem('nexchat.translate', JSON.stringify(trSettings)); } catch (e) { /* 隐私模式忽略 */ }
    }
    function curInTo() { return trSettings.inTo || pageLang; }
    // 语言码 → 站点语言清单里的显示名（清单拉不到就退回语言码，不空着）
    function langDisplay(code) {
      for (var i = 0; i < LANGS.length; i++) {
        if (LANGS[i].c === code) return LANGS[i].n || code;
      }
      return code || '';
    }

    // 译文缓存：历史消息每次开面板都重渲染，靠缓存避免重复烧限流额度（10 次/分/IP）。
    var trCache = (function () {
      try { return JSON.parse(localStorage.getItem('nexchat.tr.cache') || '{}') || {}; } catch (e) { return {}; }
    })();
    var trCacheDirty = false;
    function trKey(dir, to, text) { return dir + '|' + to + '|' + text; }
    function trCachePut(key, val) {
      var keys = Object.keys(trCache);
      if (keys.length >= 200) delete trCache[keys[0]]; // 简单 FIFO，缓存只求省额度不求命中率高
      trCache[key] = val;
      trCacheDirty = true;
    }
    function trCacheFlush() {
      if (!trCacheDirty) return;
      try { localStorage.setItem('nexchat.tr.cache', JSON.stringify(trCache)); } catch (e) { /* 超配额就放弃 */ }
      trCacheDirty = false;
    }
    // 批量翻完不等下一次操作：离开页面时也落盘，重开面板直接命中缓存
    window.addEventListener('pagehide', trCacheFlush);

    // 批量机翻：一次请求带 ≤20 条文本，dir 决定默认方向，from/to 是翻译设置的显式覆盖。
    function translateBatch(texts, dir, opts) {
      return fetch('/website/visitor/translate', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({
          texts: texts, dir: dir, lang: document.documentElement.lang || '',
          from: (opts && opts.from) || '', to: (opts && opts.to) || ''
        })
      }).then(function (res) {
        if (res.status === 429) throw new Error('rate');
        if (!res.ok) throw new Error('unavailable');
        return res.json().catch(function () { return {}; });
      }).then(function (body) {
        var data = body && body.data;
        if (!data || !Array.isArray(data.texts) || data.texts.length !== texts.length) throw new Error('bad');
        return data;
      });
    }
    function note(text) {
      if (!log) return;
      // 连点同一个动作不该刷屏：最后一条提示文字相同就跳过
      var last = log.lastElementChild;
      if (last && last.classList && last.classList.contains('chat-note') && last.textContent === text) return;
      var p = document.createElement('p');
      p.className = 'chat-note';
      p.textContent = text;
      log.appendChild(p);
      log.scrollTop = log.scrollHeight;
    }
    if (translateBtn) {
      // 文A 按钮 = 翻译快捷入口：打开快捷浮层（立即翻译输入内容 + 自动翻译开关），
      // 立即翻译的动作移进了浮层（openSettings 的 compact 分支）。
      translateBtn.addEventListener('click', function () { openSettings(true); });
    }
    // 快捷浮层开着时草稿变化要同步主按钮的可用态（关掉浮层后 syncQuickNow 会被清空）
    if (input) input.addEventListener('input', function () { if (syncQuickNow) syncQuickNow(); });

    // ---- 收到的客服消息：气泡下挂「翻译」按钮（手动），开启设置后自动整批翻译 ----
    // 把气泡换成译文并记录原文：手动按钮负责「查看原文 ↔ 译文」来回切。
    function markTranslated(bubble, btn, source, translated, to) {
      bubble.textContent = translated;
      btn.setAttribute('data-src', source);
      btn.setAttribute('data-tr-to', to || curInTo());
      btn.textContent = t('chatTranslateOriginal', '查看原文');
      btn.setAttribute('data-done', '1');
    }
    // 自动翻译管道：收集未译的客服气泡 → 命中缓存的直接换 → 余下的按 20 条一批
    // 调批量接口（匿名限流 10 次/分/IP，逐条翻 50 条历史必撞限）。失败提示后本轮停止，
    // 不自动重试——按钮还在，访客可手动重试。
    function autoTranslateAgentPass() {
      if (!log || !trSettings.autoIn) return;
      // 失败冷却：失败提示（note）追加进消息区会再触发 MutationObserver，
      // 没有冷却就是「失败→提示→重跑→再失败」的自旋，直到撞限流（实测过）。
      if (Date.now() < state.trFailUntil) return;
      var pending = [];
      Array.prototype.forEach.call(log.querySelectorAll('.chat-msg[data-from="agent"]'), function (row) {
        var btn = row.querySelector('[data-chat-translate-msg]');
        var bubble = row.querySelector('.chat-bubble');
        if (!btn || !bubble || btn.getAttribute('data-done') === '1') return;
        var to = curInTo();
        if (btn.getAttribute('data-tr-to') === to) return; // 这个目标语言已经处理过
        var source = String(bubble.textContent || '').trim();
        if (!source) return;
        // 带图片/附件/知识库来源的结构化气泡不自动翻：翻译会吃掉文件名与来源条目
        if (bubble.querySelector('.chat-image, .chat-file, .chat-sources')) return;
        var cached = trCache[trKey('in', to, source)];
        if (cached) {
          markTranslated(bubble, btn, source, cached, to);
          return;
        }
        btn.setAttribute('data-tr-to', to); // 先占位，批内失败再放开，避免同一轮重复入队
        pending.push({ bubble: bubble, btn: btn, source: source, to: to });
      });
      if (!pending.length) return;
      var stopped = false;
      function next() {
        if (stopped || !pending.length) { trCacheFlush(); return; }
        var chunk = pending.splice(0, 20);
        translateBatch(chunk.map(function (p) { return p.source; }), 'in', { to: curInTo() })
          .then(function (data) {
            if (data.skipped) return; // 同语言无需翻：占位已标记，本轮跳过即可
            data.texts.forEach(function (tr, i) {
              var p = chunk[i];
              if (!p) return;
              trCachePut(trKey('in', data.to, p.source), tr);
              markTranslated(p.bubble, p.btn, p.source, tr, data.to);
            });
          })
          .catch(function () {
            stopped = true;
            chunk.forEach(function (p) { p.btn.removeAttribute('data-tr-to'); });
            state.trFailUntil = Date.now() + 30000; // 冷却 30 秒，期间不再自动重试
            note(t('chatTranslateFailed', '翻译失败，请稍后重试'));
          })
          .then(next);
      }
      next();
    }
    function decorateMessages() {
      if (!log) return;
      // 商品 / 名牌 / 地址这类结构化消息是多行文本，给气泡加一点卡片感
      Array.prototype.forEach.call(log.querySelectorAll('.chat-bubble'), function (b) {
        var txt = String(b.textContent || '').trim();
        if (/^\[[^\]]+\]/.test(txt) && txt.indexOf('\n') > 0) b.classList.add('chat-bubble-card');
      });
      var rows = log.querySelectorAll('.chat-msg[data-from="agent"]');
      Array.prototype.forEach.call(rows, function (row) {
        if (row.querySelector('[data-chat-translate-msg]')) return;
        var bubble = row.querySelector('.chat-bubble');
        if (!bubble) return;
        var source = bubble.textContent || '';
        if (!source.trim()) return;
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'chat-msg-translate';
        btn.setAttribute('data-chat-translate-msg', '');
        btn.textContent = t('chatTranslateIn', '翻译此消息');
        btn.addEventListener('click', function () {
          if (btn.disabled) return;
          if (btn.getAttribute('data-done') === '1') {
            bubble.textContent = btn.getAttribute('data-src') || source;
            btn.textContent = t('chatTranslateIn', '翻译此消息');
            btn.removeAttribute('data-done');
            return;
          }
          // 手动翻译：先查缓存（自动翻过又切回原文的场景不再发请求），再走批量接口
          var to = curInTo();
          var cached = trCache[trKey('in', to, source)];
          if (cached) {
            markTranslated(bubble, btn, source, cached, to);
            return;
          }
          btn.disabled = true;
          translateBatch([source], 'in', { to: to }).then(function (data) {
            if (data.skipped) {
              btn.textContent = t('chatTranslateOriginal', '查看原文');
              btn.setAttribute('data-src', source);
              btn.setAttribute('data-tr-to', data.to || to);
              btn.setAttribute('data-done', '1');
              note(t('chatTranslateSame', '当前语言一致，无需翻译'));
              return;
            }
            trCachePut(trKey('in', data.to, source), data.texts[0]);
            trCacheFlush();
            markTranslated(bubble, btn, source, data.texts[0], data.to);
          }).catch(function () {
            note(t('chatTranslateFailed', '翻译失败，请稍后重试'));
          }).then(function () { btn.disabled = false; });
        });
        // 翻译入口并进元信息行：气泡下只剩一行「名字 · 时间 · 翻译此消息」
        var metaEl = row.querySelector('.chat-meta');
        if (metaEl) {
          var sep = document.createElement('span');
          sep.className = 'chat-meta-sep';
          sep.setAttribute('aria-hidden', 'true');
          sep.textContent = '·';
          metaEl.appendChild(sep);
          metaEl.appendChild(btn);
        } else {
          row.appendChild(btn);
        }

        // 头部客服身份：从消息 meta 里取坐席名，AI 回答不冒充真人
        var meta = row.querySelector('.chat-meta');
        if (meta && agentName && !state.agentName) {
          var who = String(meta.textContent || '').split('·')[0].trim();
          if (who && who !== 'AI' && who !== t('chatAgent', '客服')) {
            state.agentName = who;
            // 名字顶到标题上（对齐阿里商详：头部就是「坐席姓名 · 本地时间」一行），
            // 面板只有 380 宽，再挂「在线客服 + 姓名 + 时间」会被截断成省略号。
            if (chatTitle) chatTitle.textContent = who;
            agentName.textContent = '';
            if (agentBox) agentBox.hidden = false;
            updateTime(); // 姓名先到时补上本地时间，姓名后到时它自己会亮出来
          }
        }
      });
      autoTranslateAgentPass();
    }
    if (log && 'MutationObserver' in window) {
      var mo = new MutationObserver(function () { decorateMessages(); });
      mo.observe(log, { childList: true, subtree: false });
    }
    decorateMessages();

    // ---- 对方本地时间：IM 的服务时区由公开 status 接口下发 ----
    // 只在已经知道坐席姓名时露出来：「本地时间 23:19」单独一行看不出跟谁对话，宁可不显示。
    function updateTime() {
      if (!state.timezone || !agentTime) return;
      try {
        var s = new Intl.DateTimeFormat([], { timeZone: state.timezone, hour: '2-digit', minute: '2-digit' }).format(new Date());
        agentTime.textContent = t('chatLocalTime', '本地时间') + ' ' + s;
        if (agentBox && state.agentName) agentBox.hidden = false;
      } catch (e) { /* 时区名非法时忽略 */ }
    }
    fetch('/api/v1/plugins/im/visitor/status', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then(function (r) { return r.ok ? r.json().catch(function () { return {}; }) : {}; })
      .then(function (body) {
        var cfg = (body && body.data) || {};
        state.timezone = cfg.timezone || '';
        updateTime();
        if (state.timezone) window.setInterval(updateTime, 60000);
      }).catch(function () { /* 未装 IM 时挂件本就隐藏 */ });

    // 详情页的「数量」步进器：把访客选好的数量一并带进询盘，
    // 客服第一句就能看到要多少，不用来回问。列表页没有步进器，返回空值即可。
    function pageQty() {
      var inp = document.querySelector('[data-pd-qty-input]');
      if (!inp) return { qty: '', unit: '' };
      var box = document.querySelector('[data-pd-qty]');
      var v = parseInt(String(inp.value).replace(/[^\d]/g, ''), 10);
      return { qty: isNaN(v) ? '' : String(v), unit: box ? (box.getAttribute('data-pd-qty-unit') || '') : '' };
    }

    // ---- 商品引用卡：点详情页/列表页的「立即询盘」把商品带进会话 ----
    function renderProduct() {
      if (!productBox) return;
      if (!state.product) { productBox.hidden = true; return; }
      productBox.hidden = false;
      if (productTitle) productTitle.textContent = state.product.title || '';
      if (productPrice) productPrice.textContent = state.product.price || '';
      if (productMoq) {
        var bits = [];
        if (state.product.moq) bits.push(state.product.moq);
        if (state.product.qty) bits.push(t('chatQuantity', '数量') + ': ' + state.product.qty + (state.product.qtyUnit ? ' ' + state.product.qtyUnit : ''));
        productMoq.textContent = bits.join(' · ');
      }
      if (productCover) {
        productCover.textContent = '';
        if (state.product.cover) {
          var img = document.createElement('img');
          img.src = state.product.cover;
          img.alt = '';
          productCover.appendChild(img);
        }
      }
    }
    function clearProduct() {
      state.product = null;
      state.sent = false;
      renderProduct();
    }
    if (productRemove) productRemove.addEventListener('click', clearProduct);
    // 挂上商品后再调数量：卡片与待发消息跟着变，不会发出旧数字
    document.addEventListener('input', function (e) {
      if (!state.product || !e.target || !e.target.matches || !e.target.matches('[data-pd-qty-input]')) return;
      var q = pageQty();
      state.product.qty = q.qty;
      state.product.qtyUnit = q.unit;
      state.sent = false;
      renderProduct();
    });
    document.addEventListener('click', function (e) {
      var cta = e.target.closest ? e.target.closest('[data-chat-open-product]') : null;
      if (!cta) return;
      // 面板没渲染出来时别吞点击：<a> 还得靠 href 走邮件 / 联系页
      if (!panel) return;
      e.preventDefault();
      var q = pageQty();
      state.product = {
        title: cta.getAttribute('data-chat-title') || '',
        price: cta.getAttribute('data-chat-price') || '',
        moq: cta.getAttribute('data-chat-moq') || '',
        qty: q.qty,
        qtyUnit: q.unit,
        url: cta.getAttribute('data-chat-url') || '',
        cover: cta.getAttribute('data-chat-cover') || ''
      };
      state.sent = false;
      renderProduct();
      if (panel.hidden) {
        var launcher = root.querySelector('[data-chat-open]');
        if (launcher) launcher.click();
      }
      // 「联系商家」是直接对话：先垫一句可发送的开场白，
      // 点完输入框里立刻有字，不会像点了没反应；用户自己写过就不覆盖。
      if (cta.getAttribute('data-chat-intent') === 'contact' && input && !String(input.value || '').trim()) {
        input.value = t('chatGreeting', '您好，我想进一步了解这款产品的详细信息。');
        input.dispatchEvent(new Event('input', { bubbles: true }));
      }
      if (input) {
        input.focus();
        // 面板展开有过渡，等一帧再聚焦，键盘和光标才落得进去
        window.setTimeout(function () { input.focus(); }, 80);
      }
    });
    // capture 阶段改写输入值，让原挂件的 send() 直接把商品上下文一起发出去
    if (form) {
      form.addEventListener('submit', function () {
        if (!input || !state.product || state.sent) return;
        var lines = ['[' + t('chatProductLabel', '咨询商品') + '] ' + state.product.title];
        if (state.product.price) lines.push(state.product.price);
        if (state.product.moq) lines.push(state.product.moq);
        if (state.product.qty) lines.push(t('chatQuantity', '数量') + ': ' + state.product.qty + (state.product.qtyUnit ? ' ' + state.product.qtyUnit : ''));
        if (state.product.url) lines.push(location.origin + state.product.url);
        input.value = lines.join('\n') + '\n\n' + String(input.value || '').trim();
        state.sent = true;
      }, true);
      // capture 阶段拦截发送：开了「翻译发送的消息」就先翻译再重放提交。
      // 注册在商品上下文之后，商品卡片行已经拼进输入框，一并翻给客服看。
      // 翻译失败不挡发送：回退发原文，只提示不静默。
      form.addEventListener('submit', function (e) {
        if (state.outDone) { state.outDone = false; return; } // 重放的那次：放行给原挂件 send()
        if (!input || !trSettings.autoOut) return;
        var text = String(input.value || '').trim();
        if (!text) return;
        e.preventDefault();
        e.stopPropagation();
        var sendBtn = root.querySelector('[data-chat-send]');
        if (sendBtn) sendBtn.disabled = true;
        translateBatch([text], 'out', { from: trSettings.outFrom || '', to: trSettings.outTo || '' })
          .then(function (data) {
            if (!data.skipped) {
              input.value = data.texts[0];
              trCachePut(trKey('out', data.to, text), data.texts[0]);
              trCacheFlush();
            }
          })
          .catch(function () {
            note(t('chatTranslateFailed', '翻译失败，请稍后重试'));
          })
          .then(function () {
            if (sendBtn) sendBtn.disabled = false;
            state.outDone = true;
            // requestSubmit 会重新走一遍 submit：商品上下文已 sent 跳过，
            // 本监听器看到 outDone 直接放行，原挂件的 send() 正常发出译文。
            if (typeof form.requestSubmit === 'function') form.requestSubmit();
            else form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
          });
      }, true);
    }

    // ---- 名牌 / 地址卡片：访客填一次，之后一键复用；发送走原挂件的 send() ----
    var CARD_DEFS = {
      name: {
        title: t('chatCardNameTitle', '我的名片'),
        marker: '[' + t('chatCardNameLabel', '名片') + ']',
        // 两列网格里按「必填 → 公司 → 选填」排：原来姓名、公司、邮箱各自独占整行，
        // 中间夹着职位/电话半行，满行与半行交替，卡片看上去参差不齐。
        // 现在 1+1 / 占满 / 1+1，每行都收在同一列宽上。
        fields: [
          { key: 'name', label: t('chatCardName', '姓名'), required: true },
          // 名片没有国家选择器，电话必须自带区号才能回拨；提示语只进占位符，不进名片正文
          { key: 'phone', label: t('chatCardPhone', '电话'), required: true, ph: t('chatCardPhonePh', '国家区号 + 号码') },
          { key: 'company', label: t('chatCardCompany', '公司'), wide: true },
          { key: 'job', label: t('chatCardJob', '职位') },
          { key: 'email', label: t('chatCardEmail', '邮箱') }
        ]
      },
      address: {
        title: t('chatCardAddressTitle', '收货地址'),
        marker: '[' + t('chatCardAddressLabel', '收货地址') + ']',
        // 对齐 Alibaba 添加地址：国家/地区 → 州/省 → 城市 → 邮编（城市与邮编同一行），
        // 详细地址独占一行收尾。之前城市、邮编各自半行、右边空着，两列网格看着参差。
        fields: [
          { key: 'recipient', label: t('chatCardRecipient', '收件人'), required: true },
          { key: 'phone', label: t('chatCardPhone', '电话'), required: true },
          { key: 'country', label: t('chatCardCountry', '国家/地区'), required: true },
          { key: 'region', label: t('chatCardRegion', '州/省'), required: true },
          { key: 'city', label: t('chatCardCity', '城市'), required: true },
          { key: 'postcode', label: t('chatCardPostcode', '邮编') },
          { key: 'street', label: t('chatCardStreet', '详细地址'), required: true, wide: true }
        ]
      }
    };
    var cardPop = document.createElement('div');
    cardPop.className = 'chat-card-pop';
    cardPop.setAttribute('data-chat-card-pop', '');
    cardPop.hidden = true;
    if (panel) panel.appendChild(cardPop);
    // 校验红框在用户改动该字段时就清掉；卡片内容每次打开重建，监听挂在容器上一次就够。
    // 下拉里的搜索框不算「改了这个字段」——打字清掉红框但值仍是空的，会误导。
    cardPop.addEventListener('input', function (e) {
      var t = e.target;
      if (t && t.classList && t.classList.contains('chat-card-search')) return;
      clearFieldInvalid(t);
    });
    cardPop.addEventListener('click', function (e) {
      if (e.target && e.target.closest && e.target.closest('[data-opt]')) clearFieldInvalid(e.target);
    });
    // 下拉的取值位是隐藏 input：红框要标在可见的触发钮上
    function cardFieldControl(el) {
      if (!el) return null;
      if (el.type === 'hidden') return el.parentElement ? el.parentElement.querySelector('.chat-card-trigger') : null;
      return el;
    }
    function clearFieldInvalid(node) {
      var lab = node && node.closest ? node.closest('label') : null;
      if (!lab) return;
      Array.prototype.forEach.call(lab.querySelectorAll('.is-invalid'), function (x) {
        x.classList.remove('is-invalid');
        x.removeAttribute('aria-invalid');
      });
    }
    function showCardError(msg) {
      var p = cardPop.querySelector('.chat-card-error');
      if (!p) {
        p = document.createElement('p');
        p.className = 'chat-card-error';
        p.setAttribute('role', 'alert');
        cardPop.insertBefore(p, cardPop.querySelector('.chat-card-actions'));
      }
      p.textContent = msg;
    }
    // 打开卡片时面板是否本来就已展开：false 说明是卡片自己展开的，关卡要退回去
    var cardWasExpanded = false;
    var activeKind = null;
    // 地址卡片的下拉状态（国家对象、区号）要跨 openCard/submitCard 两个函数使用
    var addrState = null;

    // 翻译设置弹层：与名牌/地址卡同手法挂在 panel 上，内容每次打开重建（回填当前设置）
    var settingsPop = document.createElement('div');
    settingsPop.className = 'chat-settings-pop';
    settingsPop.setAttribute('data-chat-settings-pop', '');
    settingsPop.setAttribute('role', 'dialog');
    settingsPop.setAttribute('aria-label', t('chat.translate', '翻译'));
    settingsPop.setAttribute('tabindex', '-1');
    settingsPop.hidden = true;
    if (panel) panel.appendChild(settingsPop);
    // 弹层从多个入口（工具栏文A、快捷浮层里的设置、点外/Esc）开关，aria-expanded
    // 统一跟着 hidden 走，不在每个关它的地方各写一遍。
    if (translateBtn) {
      translateBtn.setAttribute('aria-haspopup', 'dialog');
      translateBtn.setAttribute('aria-expanded', 'false');
      if (window.MutationObserver) {
        new window.MutationObserver(function () {
          translateBtn.setAttribute('aria-expanded', settingsPop.hidden ? 'false' : 'true');
          if (settingsPop.hidden) syncQuickNow = null;
        }).observe(settingsPop, { attributes: true, attributeFilter: ['hidden'] });
      }
    }

    // 可搜索下拉（国家/州省）：选项由调用方灌入，选中值放在 hidden input 的
    // data-field 上 —— submitCard 仍按 [data-field] 取值，普通字段与下拉字段共用一条路径。
    // 选项不超过 12 个就不出搜索框（多数国家的州省一屏能看全）。
    function buildSelect(key, placeholder, onPick, emptyText, fallbackPh) {
      var wrap = document.createElement('div');
      wrap.className = 'chat-card-select';
      var trig = document.createElement('button');
      trig.type = 'button';
      trig.className = 'chat-card-trigger';
      trig.setAttribute('aria-haspopup', 'listbox');
      trig.setAttribute('data-select-trigger', '');
      var flagEl = document.createElement('span');
      flagEl.className = 'chat-card-trig-flag';
      var txtEl = document.createElement('span');
      txtEl.className = 'chat-card-trig-txt is-ph';
      txtEl.textContent = placeholder;
      var chev = document.createElement('span');
      chev.className = 'chat-card-trig-chev';
      chev.innerHTML = CARD_ICONS.chevron;
      trig.appendChild(flagEl); trig.appendChild(txtEl); trig.appendChild(chev);
      var hidden = document.createElement('input');
      hidden.type = 'hidden';
      hidden.setAttribute('data-field', key);
      var drop = document.createElement('div');
      drop.className = 'chat-card-drop';
      drop.hidden = true;
      var search = document.createElement('input');
      search.type = 'text';
      search.className = 'chat-card-search';
      search.setAttribute('autocomplete', 'off');
      search.hidden = true;
      var opts = document.createElement('div');
      opts.className = 'chat-card-options';
      opts.setAttribute('role', 'listbox');
      drop.appendChild(search); drop.appendChild(opts);
      wrap.appendChild(trig); wrap.appendChild(hidden); wrap.appendChild(drop);

      var items = [];
      var selKey = null;
      function render(q) {
        opts.textContent = '';
        q = String(q || '').toLowerCase();
        var n = 0;
        for (var i = 0; i < items.length; i++) {
          var it = items[i];
          if (q && it.needle.indexOf(q) < 0) continue;
          if (++n > 300) break;
          var b = document.createElement('button');
          b.type = 'button';
          b.setAttribute('role', 'option');
          b.setAttribute('data-opt', it.key);
          if (it.key === selKey) b.classList.add('is-sel');
          if (it.flag) {
            var fl = document.createElement('span');
            fl.className = 'chat-opt-flag';
            fl.textContent = it.flag;
            b.appendChild(fl);
          }
          var nm = document.createElement('span');
          nm.className = 'chat-opt-name';
          nm.textContent = it.disp;
          b.appendChild(nm);
          if (it.dial) {
            var dl = document.createElement('span');
            dl.className = 'chat-opt-dial';
            dl.textContent = it.dial;
            b.appendChild(dl);
          }
          opts.appendChild(b);
        }
        if (!opts.firstChild) {
          var empty = document.createElement('span');
          empty.className = 'chat-opt-empty';
          empty.textContent = emptyText || t('chatCardSearchEmpty', '无匹配结果');
          opts.appendChild(empty);
        }
      }
      function apply(it) {
        selKey = it.key;
        hidden.value = it.disp;
        flagEl.textContent = it.flag || '';
        txtEl.textContent = it.disp;
        txtEl.classList.remove('is-ph');
      }
      trig.addEventListener('click', function () {
        var open = drop.hidden;
        // 同时只开一个下拉，别让国家列表压住州省列表
        Array.prototype.forEach.call(document.querySelectorAll('.chat-card-drop'), function (d) { if (d !== drop) d.hidden = true; });
        drop.hidden = !open;
        trig.setAttribute('aria-expanded', open ? 'true' : 'false');
        if (!drop.hidden) {
          render(search.value);
          if (!search.hidden) search.focus();
        }
      });
      search.addEventListener('input', function () { render(search.value); });
      opts.addEventListener('click', function (e) {
        var b = e.target.closest ? e.target.closest('[data-opt]') : null;
        if (!b) return;
        var it = null;
        for (var i = 0; i < items.length; i++) {
          if (String(items[i].key) === b.getAttribute('data-opt')) { it = items[i]; break; }
        }
        if (!it) return;
        apply(it);
        drop.hidden = true;
        trig.setAttribute('aria-expanded', 'false');
        if (onPick) onPick(it);
      });
      var handle = {
        wrap: wrap,
        apply: apply,
        setItems: function (list) {
          items = list;
          search.hidden = list.length <= 12;
          search.setAttribute('placeholder', key === 'country'
            ? t('chatCardCountrySearch', '搜索国家/地区')
            : key === 'city'
              ? t('chatCardCitySearch', '搜索城市')
              : String(key).indexOf('lang-') === 0
                ? t('chatTranslatePick', '选择语言')
                : t('chatCardRegionSearch', '搜索省/州'));
          render(search.value);
        },
        clear: function () {
          selKey = null;
          hidden.value = '';
          flagEl.textContent = '';
          txtEl.textContent = placeholder;
          txtEl.classList.add('is-ph');
        },
        // 当前选中项的语言码（settings 表单保存时要用码，不是显示名）
        key: function () { return selKey; },
        // 数据拉不到，或存过的不在数据集里的旧手写值：整体退回普通文本框，不挡发卡
        fallbackText: function (val) {
          handle.dead = true;
          var inp = document.createElement('input');
          inp.type = 'text';
          inp.value = val || '';
          // 兜底框带上提示语：只有一行空输入没有任何说明，看起来像控件坏了
          if (fallbackPh) inp.placeholder = fallbackPh;
          inp.setAttribute('data-field', key);
          inp.setAttribute('autocomplete', 'off');
          wrap.replaceWith(inp);
          if (val) inp.focus();
        }
      };
      return handle;
    }

    function closeOpenDrops() {
      var closed = false;
      Array.prototype.forEach.call(cardPop.querySelectorAll('.chat-card-drop'), function (d) {
        if (!d.hidden) { d.hidden = true; closed = true; }
      });
      Array.prototype.forEach.call(cardPop.querySelectorAll('[data-select-trigger]'), function (tg) {
        tg.setAttribute('aria-expanded', 'false');
      });
      return closed;
    }
    // Esc 有两个监听器（历史遗留），一次按键两个都会跑：第一个收掉下拉时记下事件
    // timeStamp，第二个凭同一事件号跳过，才能做到「先收下拉、再收卡片」分两步。
    var escDropAt = -1;
    function escForCard(e) {
      if (!cardPop || cardPop.hidden) return false;
      if (closeOpenDrops()) { escDropAt = e.timeStamp; return true; }
      if (e.timeStamp === escDropAt) return true;
      return false;
    }

    function readCard(kind) {
      try { return JSON.parse(localStorage.getItem('nexchat.card.' + kind) || '{}') || {}; }
      catch (e) { return {}; }
    }
    function saveCard(kind, vals) {
      try { localStorage.setItem('nexchat.card.' + kind, JSON.stringify(vals)); } catch (e) { /* 隐私模式忽略 */ }
    }
    function closeCard() {
      cardPop.hidden = true;
      activeKind = null;
      // 卡片是自己展开面板的，关掉就还原；期间用户手动收起过（expanded 已为 false）则不动
      if (!cardWasExpanded && state.expanded && expandBtn) expandBtn.click();
    }

    function openCard(kind) {
      if (activeKind === kind && !cardPop.hidden) { closeCard(); return; }
      if (emojiPop) emojiPop.hidden = true;
      if (settingsPop) settingsPop.hidden = true;
      // 地址表单字段多，380px 收起面板里挤成一团：面板不在展开态就先展开，
      // 关卡时按 cardWasExpanded 还原。展开切换走 expandBtn.click()，
      // 面板宽度的过渡动画自己会跟上。
      cardWasExpanded = state.expanded;
      if (!cardWasExpanded && expandBtn) expandBtn.click();
      activeKind = kind;
      var def = CARD_DEFS[kind];
      var saved = readCard(kind);
      cardPop.textContent = '';
      var h = document.createElement('h3'); h.textContent = def.title; cardPop.appendChild(h);
      var hint = document.createElement('p');
      hint.className = 'chat-card-hint';
      hint.textContent = t('chatCardHint', '信息仅用于本次询盘沟通');
      cardPop.appendChild(hint);
      var grid = document.createElement('div'); grid.className = 'chat-card-fields';
      // 地址卡片的国家/州省/电话走专用控件：对齐阿里「添加地址」的形态 ——
      // 国家带国旗可搜索、州省跟着国家换成下拉、电话自动带区号。
      // 数据是异步的：先铺好空下拉，拉到数据再灌选项并回填存过的值。
      var addr = { country: null, dial: '', dialEl: null, citySel: null, cityData: null, cityDataReady: false, curRegion: '', pendingRegion: '', pendingCity: '' };
      addrState = kind === 'address' ? addr : null;
      var regionSel = null;
      function dispName(c) { return siteLangEn() ? c.en : c.zh; }
      // 城市下拉跟随所选州：该州有城市数据（GeoNames，按国家拆文件）就给下拉，
      // 没有就退回手写输入。城市数据未到位前保持占位，等 loadCityData 回调再灌。
      function syncCities(stateName) {
        addr.curRegion = stateName || '';
        if (!addr.citySel || addr.citySel.dead) return;
        if (!addr.curRegion || !addr.cityDataReady) { addr.citySel.clear(); return; }
        var list = addr.cityData[stateName];
        if (!list || !list.length) {
          addr.citySel.fallbackText(addr.pendingCity || '');
          return;
        }
        addr.citySel.setItems(list.map(function (nm) { return { key: nm, disp: nm, needle: nm.toLowerCase() }; }));
        if (addr.pendingCity && list.indexOf(addr.pendingCity) >= 0) {
          addr.citySel.apply({ key: addr.pendingCity, disp: addr.pendingCity, needle: addr.pendingCity.toLowerCase() });
        } else {
          addr.citySel.clear();
        }
        addr.pendingCity = '';
      }
      function loadCityData(code) {
        loadCities(code).then(function (data) {
          // 等待期间国家又被换掉的话以最后一次为准（旧回调直接作废）
          if (addr.country && addr.country.c === code) {
            addr.cityData = data;
            addr.cityDataReady = true;
            if (!data) {
              // 该国没有城市数据文件：城市直接退回手写输入，不必等选州
              if (addr.citySel && !addr.citySel.dead) addr.citySel.fallbackText(addr.pendingCity || '');
              return;
            }
            syncCities(addr.curRegion);
          }
        });
      }
      function syncRegion(c) {
        if (!regionSel || regionSel.dead) return;
        var rs = c.r || [];
        if (!rs.length) {
          // 该国没有州省数据（如一些小国）：州与城市一起退回手写输入，与阿里的行为一致
          regionSel.fallbackText(addr.pendingRegion || '');
          if (addr.citySel && !addr.citySel.dead) addr.citySel.fallbackText(addr.pendingCity || '');
          return;
        }
        regionSel.setItems(rs.map(function (nm) { return { key: nm, disp: nm, needle: nm.toLowerCase() }; }));
        if (addr.pendingRegion && rs.indexOf(addr.pendingRegion) >= 0) {
          regionSel.apply({ key: addr.pendingRegion, disp: addr.pendingRegion, needle: addr.pendingRegion.toLowerCase() });
          syncCities(addr.pendingRegion);
        } else {
          regionSel.clear();
          syncCities('');
        }
        addr.pendingRegion = '';
      }
      function applyCountry(c, isRestore) {
        // 用户换国家：城市跟着清空重选（阿里同款行为）；开卡回填走 isRestore，
        // 已存的城市值留在 pendingCity 里等城市数据到位后自动回填
        if (!isRestore && addr.country && addr.country.c !== c.c && addr.citySel && !addr.citySel.dead) {
          addr.citySel.clear();
          addr.pendingCity = '';
        }
        addr.country = c;
        addr.dial = c.dial || '';
        if (addr.dialEl) {
          addr.dialEl.textContent = addr.dial;
          addr.dialEl.hidden = !addr.dial;
        }
        syncRegion(c);
        loadCityData(c.c);
      }
      function buildCountry(label, savedVal) {
        var sel = buildSelect('country', t('chatCardCountrySelect', '选择国家/地区'), function (it) {
          applyCountry(it.raw, false);
        }, '', t('chatCardCountryInput', '输入国家/地区'));
        label.appendChild(sel.wrap);
        loadCountries().then(function (list) {
          var items = list.map(function (c) {
            return {
              key: c.c, disp: dispName(c), flag: flagEmoji(c.c), dial: c.dial, raw: c,
              needle: (c.zh + ' ' + c.en + ' ' + c.c + ' ' + (c.dial || '')).toLowerCase()
            };
          });
          sel.setItems(items);
          // 无存档国家时退回头部「交货至」记的国家（ISO2 码，site.js 写入）：
          // 买家在头部选过一次，地址卡就不必从头挑。
          var shipCode = '';
          try { shipCode = localStorage.getItem('nex-site-shipto') || ''; } catch (e) { /* 隐私模式忽略 */ }
          var hit = null;
          for (var i = 0; i < items.length; i++) {
            if (items[i].raw.zh === savedVal || items[i].raw.en === savedVal) { hit = items[i]; break; }
          }
          if (!hit && shipCode) {
            for (var j = 0; j < items.length; j++) {
              if (items[j].raw.c === shipCode) { hit = items[j]; break; }
            }
          }
          if (hit) {
            sel.apply(hit);
            applyCountry(hit.raw, true);
          } else if (savedVal) {
            sel.fallbackText(savedVal); // 旧手写值不在数据集里，保持可编辑
          }
        }).catch(function () {
          sel.fallbackText(savedVal); // 数据不可用也不挡发卡
        });
      }
      function buildRegion(label, savedVal) {
        addr.pendingRegion = savedVal || '';
        var sel = buildSelect('region', t('chatCardRegionSelect', '选择省/州'), function (it) {
          syncCities(it.key);
        });
        regionSel = sel;
        label.appendChild(sel.wrap);
      }
      function buildCity(label, savedVal) {
        addr.pendingCity = savedVal || '';
        var sel = buildSelect('city', t('chatCardCitySelect', '选择城市'), function () { /* 城市无下级联动 */ }, t('chatCardCityEmpty', '先选择州/省'));
        addr.citySel = sel;
        label.appendChild(sel.wrap);
      }
      function buildPhone(label, saved) {
        var raw = String(saved.phone || '');
        // 上次提交时已拼过区号，回显时剥掉，避免前缀块 + 输入值重复
        if (saved.phoneDial && raw.indexOf(saved.phoneDial + ' ') === 0) raw = raw.slice(saved.phoneDial.length + 1);
        var wrap = document.createElement('div');
        wrap.className = 'chat-phone-wrap';
        var dial = document.createElement('span');
        dial.className = 'chat-phone-dial';
        dial.hidden = true;
        var inp = document.createElement('input');
        inp.type = 'tel';
        inp.value = raw;
        inp.setAttribute('data-field', 'phone');
        inp.setAttribute('autocomplete', 'off');
        wrap.appendChild(dial); wrap.appendChild(inp);
        label.appendChild(wrap);
        addr.dialEl = dial;
        if (saved.phoneDial) {
          addr.dial = saved.phoneDial;
          dial.textContent = saved.phoneDial;
          dial.hidden = false;
        }
      }
      def.fields.forEach(function (f) {
        var label = document.createElement('label');
        if (f.wide) label.className = 'wide';
        var head = document.createElement('span');
        head.className = 'chat-card-label';
        var span = document.createElement('span');
        span.textContent = f.label;
        head.appendChild(span);
        if (f.required) {
          // 星号只做视觉提示，读屏靠控件的 aria-required
          var req = document.createElement('i');
          req.className = 'req';
          req.setAttribute('aria-hidden', 'true');
          req.textContent = '*';
          head.appendChild(req);
        }
        label.appendChild(head);
        if (kind === 'address' && f.key === 'country') buildCountry(label, saved.country);
        else if (kind === 'address' && f.key === 'region') buildRegion(label, saved.region);
        else if (kind === 'address' && f.key === 'city') buildCity(label, saved.city);
        else if (kind === 'address' && f.key === 'phone') buildPhone(label, saved);
        else {
          var inp = document.createElement('input');
          inp.type = f.key === 'email' ? 'email' : (f.key === 'phone' ? 'tel' : 'text');
          inp.value = saved[f.key] || '';
          if (f.ph) inp.placeholder = f.ph;
          inp.setAttribute('data-field', f.key);
          inp.setAttribute('autocomplete', 'off');
          label.appendChild(inp);
        }
        if (f.required) {
          var ctl = label.querySelector('input:not([type="hidden"]),.chat-card-trigger');
          if (ctl) ctl.setAttribute('aria-required', 'true');
        }
        grid.appendChild(label);
      });
      cardPop.appendChild(grid);
      var actions = document.createElement('div'); actions.className = 'chat-card-actions';
      var cancel = document.createElement('button');
      cancel.type = 'button'; cancel.textContent = t('chatCardCancel', '取消');
      cancel.addEventListener('click', closeCard);
      var send = document.createElement('button');
      send.type = 'button'; send.className = 'is-primary';
      send.textContent = t('chatCardSend', '发送卡片');
      send.addEventListener('click', function () { submitCard(kind); });
      actions.appendChild(cancel); actions.appendChild(send);
      cardPop.appendChild(actions);
      cardPop.hidden = false;
      var first = cardPop.querySelector('input[type="text"],input[type="tel"],input[type="email"]');
      if (first) first.focus();
    }

    function submitCard(kind) {
      var def = CARD_DEFS[kind], vals = {}, firstBad = null;
      def.fields.forEach(function (f) {
        var el = cardPop.querySelector('[data-field="' + f.key + '"]');
        var v = el ? String(el.value || '').trim() : '';
        vals[f.key] = v;
        if (f.required && !v) {
          var ctl = cardFieldControl(el);
          if (ctl) {
            ctl.classList.add('is-invalid');
            ctl.setAttribute('aria-invalid', 'true');
            if (!firstBad) firstBad = ctl;
          }
        }
      });
      // 必填提示必须落在卡片里：note() 写进的消息区正被这张卡片盖住，
      // 用户只会看到「点了发送没反应」。
      if (firstBad) {
        showCardError(t('chatCardRequired', '请填写必填项'));
        firstBad.focus();
        return;
      }
      // 地址卡片：把区号拼进电话值（用户自己带了 + 的不重复拼）；phoneDial 只进
      // localStorage（下次开卡还原前缀块），不进消息正文，消息里已含在电话值里。
      if (kind === 'address' && addrState) {
        vals.phoneDial = addrState.dial;
        if (vals.phone && addrState.dial && vals.phone.charAt(0) !== '+') {
          vals.phone = addrState.dial + ' ' + vals.phone;
        }
      }
      saveCard(kind, vals);
      var lines = [def.marker];
      def.fields.forEach(function (f) { if (vals[f.key]) lines.push(f.label + ': ' + vals[f.key]); });
      var text = lines.join('\n');
      if (input) {
        var cur = String(input.value || '').trim();
        input.value = cur ? cur + '\n\n' + text : text;
      }
      closeCard();
      // requestSubmit 会触发 form 的 submit 事件：商品引用上下文的 capture 监听与
      // 原挂件 send() 都会照常执行，不需要复制任何会话逻辑。
      if (form && typeof form.requestSubmit === 'function') form.requestSubmit();
      else { var sb = root.querySelector('[data-chat-send]'); if (sb) sb.click(); }
    }

    var bar = root.querySelector('.chat-compose-bar');
    if (bar && panel) {
      var frag = document.createDocumentFragment();
      [['name', CARD_ICONS.name, t('chatNameCard', '名牌卡片')],
       ['address', CARD_ICONS.address, t('chatAddressCard', '地址卡片')]].forEach(function (item) {
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'chat-tool chat-card-btn';
        btn.setAttribute('data-chat-card-btn', item[0]);
        btn.setAttribute('aria-label', item[2]);
        btn.title = item[2];
        btn.innerHTML = item[1];
        btn.addEventListener('click', function () { openCard(item[0]); });
        frag.appendChild(btn);
      });
      bar.insertBefore(frag, bar.firstChild);
      document.addEventListener('click', function (e) {
        if (cardPop.hidden) return;
        if (cardPop.contains(e.target) || (e.target.closest && e.target.closest('[data-chat-card-btn]'))) return;
        closeCard();
      });
    }

    // ---- 翻译设置弹层（对齐阿里聊天窗口的「翻译设置」）----
    // 收到的消息：自动把客服消息翻成所选语言（批量管道在 decorateMessages 里）；
    // 发送的消息：发送时自动翻成对方语言（capture 拦截在 form submit 上）。
    // 选项数据来自页面 #site-langs；清单拉不到就只显示当前值，设置仍可开关。
    function closeSettings() { settingsPop.hidden = true; syncQuickNow = null; }
    function focusSettingsPop() {
      // 打开弹层把焦点收进容器（tabindex=-1）：文A 后面 DOM 里只剩附件和发送，
      // 不接管焦点的话键盘用户 Tab 不进来。preventScroll 防小窗里页面被滚走。
      try { settingsPop.focus({ preventScroll: true }); } catch (e) { settingsPop.focus(); }
    }
    // Esc 收弹层：先记焦点在不在里面再隐藏——display:none 会先把焦点甩给 body
    function closeSettingsByKey() {
      if (settingsPop.hidden) return;
      var inside = settingsPop.contains(document.activeElement);
      settingsPop.hidden = true;
      if (inside && translateBtn) translateBtn.focus();
    }
    // compact 模式是工具栏「翻译」按钮的快捷浮层：立即翻译输入内容 + 两个自动翻译
    // 开关 + 进完整设置的入口——原来文A 按钮直接翻译、设置藏在更多菜单，两个入口
    // 看起来像同一功能的两份，融合成一个浮层。
    var syncQuickNow = null;
    function openSettings(compact) {
      if (!settingsPop.hidden) { closeSettings(); return; }
      if (cardPop) cardPop.hidden = true;
      if (emojiPop) emojiPop.hidden = true;
      var cur = {
        autoIn: trSettings.autoIn,
        inTo: curInTo(),
        autoOut: trSettings.autoOut,
        outFrom: trSettings.outFrom || pageLang,
        outTo: trSettings.outTo || siteDefaultLang || pageLang
      };
      settingsPop.textContent = '';
      settingsPop.classList.toggle('is-quick', !!compact);
      if (compact) {
        // 快捷浮层：标题行（右侧进完整设置）+ 立即翻译 + 两个自动翻译开关。
        // 开关变化即时保存；autoIn 变化后跑一遍管道，把未翻的客服消息补上。
        var headRow = document.createElement('div');
        headRow.className = 'chat-tr-head';
        var headTitle = document.createElement('h3');
        headTitle.textContent = t('chat.translate', '翻译');
        var link = document.createElement('button');
        link.type = 'button';
        link.className = 'chat-tr-more';
        link.textContent = t('chatTranslateOptions', '设置…');
        // 外点监听按这个属性豁免入口按钮；没有它，click 冒泡到 document 会把刚打开的表单关掉
        link.setAttribute('data-chat-translate-settings', '');
        // openSettings 的 toggle 守卫会拦下「浮层开着时再调 openSettings」——先隐身再进完整表单
        link.addEventListener('click', function () { settingsPop.hidden = true; openSettings(false); });
        headRow.appendChild(headTitle);
        headRow.appendChild(link);
        settingsPop.appendChild(headRow);

        // 主按钮带上目标语言：原来只说「翻译输入内容」，目标语言藏在设置里，用户不知道译成什么。
        // 语言清单缺失时 langDisplay 只能回语言码，就别把「翻译为 zh-CN」这种码露给访客。
        var toName = langDisplay(cur.outTo);
        var idleLabel = toName && toName !== cur.outTo
          ? t('chatTranslateNowTo', '翻译为 %s').replace('%s', toName)
          : t('chatTranslateNow', '翻译输入内容');
        var busyLabel = t('chatTranslating', '翻译中…');
        var nowBtn = document.createElement('button');
        nowBtn.type = 'button';
        nowBtn.className = 'chat-tr-now';
        nowBtn.textContent = idleLabel;
        var busy = false;
        // 空草稿时按钮是禁用态：之前看着可点，点了静默无反应
        syncQuickNow = function () {
          nowBtn.disabled = busy || !String((input && input.value) || '').trim();
        };
        syncQuickNow();
        settingsPop.appendChild(nowBtn);
        // 翻译结果就地提示：写进消息区会留下一条条「当前语言一致」的药丸（连点就连串）
        var trMsg = document.createElement('p');
        trMsg.className = 'chat-tr-msg';
        trMsg.hidden = true;
        settingsPop.appendChild(trMsg);
        var trMsgTimer = 0;
        var showTrMsg = function (text, isErr) {
          trMsg.textContent = text;
          trMsg.classList.toggle('is-err', !!isErr);
          trMsg.hidden = false;
          window.clearTimeout(trMsgTimer);
          trMsgTimer = window.setTimeout(function () { trMsg.hidden = true; }, 2400);
        };

        var quickRow = function (label, key, onChange) {
          var row = document.createElement('label');
          row.className = 'chat-tr-row';
          var sw = document.createElement('input');
          sw.type = 'checkbox';
          sw.className = 'chat-switch';
          sw.checked = trSettings[key];
          sw.addEventListener('change', function () {
            trSettings[key] = sw.checked;
            saveTrSettings();
            if (onChange) onChange();
          });
          row.appendChild(sw);
          row.appendChild(document.createTextNode(label));
          settingsPop.appendChild(row);
        };
        quickRow(t('chatTranslateAutoIn', '收到的消息自动翻译'), 'autoIn', autoTranslateAgentPass);
        quickRow(t('chatTranslateAutoOut', '自动翻译我发送的消息'), 'autoOut', null);
        nowBtn.addEventListener('click', function () {
          var text = String((input && input.value) || '').trim();
          if (!text || busy) return;
          busy = true;
          nowBtn.textContent = busyLabel;
          syncQuickNow();
          translateBatch([text], 'out', { from: trSettings.outFrom || '', to: trSettings.outTo || '' })
            .then(function (data) {
              if (data.skipped) {
                showTrMsg(t('chatTranslateSame', '当前语言一致，无需翻译'));
                return;
              }
              if (input) input.value = data.texts[0];
              // 不往消息区写「已翻译」：输入框里的文字变了就是反馈，日志留给真正的对话
              // 翻完就收起，用户直接看输入框里的译文、接着发送
              settingsPop.hidden = true;
              if (input) input.focus();
            }).catch(function () {
              showTrMsg(t('chatTranslateFailed', '翻译失败，请稍后重试'), true);
            }).then(function () {
              busy = false;
              nowBtn.textContent = idleLabel;
              syncQuickNow();
            });
        });
        settingsPop.hidden = false;
        focusSettingsPop();
        return;
      }
      var h = document.createElement('h3');
      h.textContent = t('chatTranslateSettings', '翻译设置');
      settingsPop.appendChild(h);

      // 语言选择用挂件自己的下拉（和地址卡同款）：原生 <select> 的展开层是系统画的，
      // 选项高亮是系统蓝，跟这套 Canvas/currentColor 的浮层完全不是一路。
      // 返回 { el, value() }：el 放进表单，保存时读 value()（语言码）。
      var langPickSeq = 0;
      function langSelect(value) {
        var opts = LANGS.length ? LANGS : [{ c: value, n: value }];
        var picker = buildSelect('lang-' + (++langPickSeq), t('chatTranslatePick', '选择语言'), null, '', '');
        picker.setItems(opts.map(function (l) {
          return {
            key: l.c,
            disp: l.n || l.c,
            needle: String((l.n || '') + ' ' + l.c).toLowerCase()
          };
        }));
        var hit = null;
        for (var i = 0; i < opts.length; i++) {
          if (opts[i].c === value) { hit = opts[i]; break; }
        }
        if (hit) picker.apply({ key: hit.c, disp: hit.n || hit.c });
        else if (value) picker.apply({ key: value, disp: value }); // 存过的语言码不在清单里：保底显示原值
        return { el: picker.wrap, value: picker.key };
      }
      function groupRow(label, checked) {
        var row = document.createElement('div');
        row.className = 'chat-settings-row';
        var strong = document.createElement('strong');
        strong.textContent = label;
        var sw = document.createElement('input');
        sw.type = 'checkbox';
        sw.className = 'chat-switch';
        sw.checked = checked;
        row.appendChild(strong);
        row.appendChild(sw);
        return { row: row, sw: sw };
      }
      function fieldsRow(parts) {
        var row = document.createElement('div');
        row.className = 'chat-settings-fields';
        parts.forEach(function (p) {
          if (typeof p === 'string') {
            var sp = document.createElement('span');
            sp.textContent = p;
            row.appendChild(sp);
          } else row.appendChild(p);
        });
        return row;
      }
      function hint(text) {
        var p = document.createElement('p');
        p.className = 'chat-settings-hint';
        p.textContent = text;
        return p;
      }

      var inGroup = groupRow(t('chatTranslateAutoIn', '翻译收到的消息'), cur.autoIn);
      var inToSel = langSelect(cur.inTo);
      settingsPop.appendChild(inGroup.row);
      settingsPop.appendChild(fieldsRow([t('chatTranslateInTo', '翻译为'), inToSel.el]));
      settingsPop.appendChild(hint(t('chatTranslateHintIn', '收到的消息将自动翻译成您选择的语言；原有消息可在气泡下方手动切换原文。')));

      var outGroup = groupRow(t('chatTranslateAutoOut', '翻译发送的消息'), cur.autoOut);
      var outFromSel = langSelect(cur.outFrom);
      var outToSel = langSelect(cur.outTo);
      settingsPop.appendChild(outGroup.row);
      settingsPop.appendChild(fieldsRow([t('chatTranslateOutFrom', '从'), outFromSel.el, t('chatTranslateOutTo', '翻译成'), outToSel.el]));
      settingsPop.appendChild(hint(t('chatTranslateHintOut', '发送前将自动翻译成所选语言；翻译失败时会发送原文。')));

      var actions = document.createElement('div');
      actions.className = 'chat-settings-actions';
      var save = document.createElement('button');
      save.type = 'button';
      save.className = 'is-primary';
      save.textContent = t('chatTranslateSave', '保存并应用');
      save.addEventListener('click', function () {
        trSettings.autoIn = inGroup.sw.checked;
        trSettings.inTo = inToSel.value();
        trSettings.autoOut = outGroup.sw.checked;
        trSettings.outFrom = outFromSel.value();
        trSettings.outTo = outToSel.value();
        saveTrSettings();
        closeSettings();
        // 目标语言变了要让自动管道重新评估：已翻的气泡保留（按钮可切回原文），
        // 未翻的按新目标语言补翻。
        autoTranslateAgentPass();
      });
      actions.appendChild(save);
      settingsPop.appendChild(actions);
      settingsPop.hidden = false;
      focusSettingsPop();
    }
    // 翻译只有一个入口：底部工具栏的文A（快捷浮层，内含「翻译设置…」进完整表单）。
    // 原来更多菜单里还有一个「翻译设置」，两个入口位置分散、点开的东西也不一样，
    // 用户反馈「两个红的乱跑」——删掉菜单项，避免同一功能出现两份入口。

    // ---- 表情与更多菜单 ----
    // 表情按钮懒构建：不占每次页面加载的成本，挂件打开后空闲时才铺按钮；
    // 选中用浮层上的一个事件委托，不再给 176 个按钮各自挂监听。
    var emojiBuilt = false;
    var buildEmojis = function () {
      if (emojiBuilt) return;
      emojiBuilt = true;
      EMOJIS.forEach(function (em) {
        var b = document.createElement('button');
        b.type = 'button';
        b.textContent = em;
        b.title = em;
        b.setAttribute('aria-label', em);
        b.setAttribute('data-em', em);
        emojiPop.appendChild(b);
      });
    };
    // 首开的停顿不在建 DOM（实测 ~2ms），而在 176 个 emoji 字形的首次布局＋光栅化
    // （实测 ~260ms，点开那一刻才做就是「卡一下」）。挂件打开后空闲预热一次：
    // 透明显示一帧把字形画进缓存，之后点开只剩重新显示（实测 ~27ms）。
    var emojiWarming = false;
    var emojiWarmDone = false;
    var emojiWarmQueued = false;
    var stopEmojiWarm = function () {
      if (!emojiWarming) return;
      emojiWarming = false;
      emojiPop.hidden = true;
      emojiPop.style.opacity = '';
      emojiPop.style.pointerEvents = '';
    };
    var warmEmojis = function () {
      if (emojiWarmDone || !emojiPop || !panel || panel.hidden || !emojiPop.hidden) return;
      emojiWarmDone = true;
      buildEmojis();
      emojiWarming = true;
      emojiPop.style.opacity = '0';
      emojiPop.style.pointerEvents = 'none';
      emojiPop.hidden = false;
      void emojiPop.offsetHeight;
      var raf = window.requestAnimationFrame || function (cb) { return window.setTimeout(cb, 16); };
      raf(function () { raf(stopEmojiWarm); });
    };
    var scheduleEmojiWarm = function () {
      if (emojiWarmDone || emojiWarmQueued) return;
      emojiWarmQueued = true;
      // 固定 200ms：主题页的大图/进场动画会把 requestIdleCallback 一直往后推
      // （最迟 1.5s 超时才跑），而用户往往在面板打开后一两秒就点表情，正好撞上冷启动。
      // 200ms 落在面板进场动画（180ms）之后，预热不打断动画，又赶在点击之前。
      window.setTimeout(function () { emojiWarmQueued = false; warmEmojis(); }, 200);
    };
    if (emojiPop) {
      emojiPop.addEventListener('click', function (e) {
        var em = e.target && e.target.getAttribute && e.target.getAttribute('data-em');
        if (!em || !input) return;
        input.value = String(input.value || '') + em;
        // 选完就收起：浮层压在消息区上，留着挡内容；要连发再点一次按钮
        emojiPop.hidden = true;
        input.focus();
      });
    }
    if (emojiBtn && emojiPop) {
      emojiBtn.addEventListener('click', function () {
        // 预热收尾前用户就点了：接管这块浮层，别让收尾把它再藏回去
        var wasWarming = emojiWarming;
        if (wasWarming) {
          emojiWarming = false;
          emojiPop.style.opacity = '';
          emojiPop.style.pointerEvents = '';
        }
        if (!wasWarming && !emojiPop.hidden) { emojiPop.hidden = true; return; }
        buildEmojis();
        emojiPop.hidden = false;
        if (cardPop) cardPop.hidden = true;
        if (settingsPop) settingsPop.hidden = true;
      });
      // 面板是点开才出现的：一露头就排一次空闲预热，只做一次
      if (panel) {
        if (!panel.hidden) scheduleEmojiWarm();
        if (window.MutationObserver) {
          new window.MutationObserver(function () {
            if (!panel.hidden) scheduleEmojiWarm();
          }).observe(panel, { attributes: true, attributeFilter: ['hidden'] });
        }
      }
    }
    // 点浮层以外的任何地方都收起：原来只有「再点一次按钮」才关，
    // 点聊天区、点页面空白、换了别的话题，这块大浮层就一直杵在消息上面。
    document.addEventListener('click', function (e) {
      var t = e.target;
      var inEmoji = t && t.closest && t.closest('[data-chat-emoji]');
      if (emojiPop && !emojiPop.hidden && !emojiPop.contains(t) && !inEmoji) emojiPop.hidden = true;
      // 翻译设置 / 翻译快捷浮层：点外面就收（文A 入口按钮与菜单里的设置项都不算外面）
      if (settingsPop && !settingsPop.hidden &&
          !settingsPop.contains(t) && !(t && t.closest && t.closest('[data-chat-translate-settings], [data-chat-translate]'))) settingsPop.hidden = true;
      // 下拉展开层点外面就收：卡片区内部的点击（点别的字段）也算外面
      if (cardPop && !cardPop.hidden) {
        Array.prototype.forEach.call(cardPop.querySelectorAll('.chat-card-drop'), function (d) {
          if (!d.hidden && !d.parentElement.contains(t)) d.hidden = true;
        });
      }
      // 翻译设置里的语言下拉同理（浮层本身不关，只收起下拉）
      if (settingsPop && !settingsPop.hidden) {
        Array.prototype.forEach.call(settingsPop.querySelectorAll('.chat-card-drop'), function (d) {
          if (!d.hidden && !d.parentElement.contains(t)) d.hidden = true;
        });
      }
    });
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      if (emojiPop) emojiPop.hidden = true;
      closeSettingsByKey();
      // Esc 先收下拉再收卡片：连按两次才把整个地址卡关掉
      if (escForCard(e)) return;
      if (cardPop && !cardPop.hidden) closeCard();
    });
    // 翻译设置弹层贴着头部下沿展开：头部高度会随「客服姓名 + 本地时间」那一行出现而变，
    // 写死 top 就会压在标题和 × 上面，交给 ResizeObserver 实时量。
    var headEl = root.querySelector('.chat-head');
    if (headEl) {
      var syncHeadH = function () { panel.style.setProperty('--chat-head-h', headEl.offsetHeight + 'px'); };
      syncHeadH();
      if (window.ResizeObserver) new window.ResizeObserver(syncHeadH).observe(headEl);
    }
    if (expandBtn) {
      expandBtn.addEventListener('click', function () {
        state.expanded = !state.expanded;
        root.classList.toggle('is-expanded', state.expanded);
        // 图标（放大 ↔ 还原）由 CSS 按 .is-expanded 切，脚本只换可达名字，
        // 别像以前那样写 textContent —— 那会把按钮里的图标整个抹掉。
        var label = state.expanded ? t('chatCollapse', '还原窗口') : t('chatExpand', '放大窗口');
        expandBtn.setAttribute('aria-label', label);
        expandBtn.setAttribute('title', label);
        expandBtn.setAttribute('aria-pressed', state.expanded ? 'true' : 'false');
      });
    }
    if (clearBtn) {
      clearBtn.addEventListener('click', function () {
        if (input) input.value = '';
        clearProduct();
        if (input) input.focus();
      });
    }
    function inNode(target, sel) {
      return target && target.closest ? target.closest(sel) : null;
    }
    document.addEventListener('click', function (e) {
      if (emojiPop && !emojiPop.hidden &&
          !inNode(e.target, '[data-chat-emoji-pop]') && !inNode(e.target, '[data-chat-emoji]')) emojiPop.hidden = true;
    });
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      if (emojiPop) emojiPop.hidden = true;
      closeSettingsByKey();
      // Esc 先收下拉再收卡片（与上面第一个 Esc 处理器同口径）
      if (escForCard(e)) return;
      closeCard();
      // 放大后的窗口用 Esc 还原，和浮层一样不必再去点菜单。
      if (state.expanded && expandBtn) expandBtn.click();
    });
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
