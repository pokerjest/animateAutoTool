export type MascotScene =
  | 'brand'
  | 'login'
  | 'setup-account'
  | 'setup-downloader'
  | 'setup-library'
  | 'empty-library'
  | 'empty-search'
  | 'empty-playback'
  | 'organizing'
  | 'diagnosing'
  | 'error'
  | 'success'
  | 'assistant-idle'
  | 'assistant-thinking'
  | 'assistant-unread'
  | 'assistant-error'
  | 'assistant-success'
  | 'assistant-sleepy'

export type MascotAssetKind = 'brand' | 'action' | 'expression'

export interface MascotAsset {
  src: string
  alt: string
  kind: MascotAssetKind
}

const asset = (src: string, alt: string, kind: MascotAssetKind): MascotAsset => ({ src, alt, kind })

const scenes: Record<MascotScene, MascotAsset> = {
  brand: asset('/mascot/current/expressions/wink.png', 'AnimateTool 角色', 'expression'),
  login: asset('/mascot/current/actions/present.png', '正在介绍 AnimateTool', 'action'),
  'setup-account': asset('/mascot/current/expressions/confident.png', '专注保护账户的 AnimateTool 角色', 'expression'),
  'setup-downloader': asset('/mascot/current/actions/organizing.png', '正在整理下载器设置的 AnimateTool 角色', 'action'),
  'setup-library': asset('/mascot/current/actions/present.png', '正在介绍媒体目录的 AnimateTool 角色', 'action'),
  'empty-library': asset('/mascot/current/actions/sleepy-idle.png', '媒体库正在等待内容', 'action'),
  'empty-search': asset('/mascot/current/expressions/confused.png', '没有找到匹配内容', 'expression'),
  'empty-playback': asset('/mascot/current/actions/walking.png', '还没有可播放的内容', 'action'),
  organizing: asset('/mascot/current/actions/organizing.png', '正在整理媒体文件', 'action'),
  diagnosing: asset('/mascot/current/actions/troubleshooting.png', '正在检查问题', 'action'),
  error: asset('/mascot/current/expressions/worried.png', '遇到需要处理的问题', 'expression'),
  success: asset('/mascot/current/actions/success.png', '操作已完成', 'action'),
  'assistant-idle': asset('/mascot/current/expressions/cheerful.png', 'AnimateTool 助手', 'expression'),
  'assistant-thinking': asset('/mascot/current/expressions/focused.png', 'AnimateTool 助手正在思考', 'expression'),
  'assistant-unread': asset('/mascot/current/expressions/surprised.png', 'AnimateTool 助手有新回复', 'expression'),
  'assistant-error': asset('/mascot/current/expressions/worried.png', 'AnimateTool 助手请求失败', 'expression'),
  'assistant-success': asset('/mascot/current/expressions/success.png', 'AnimateTool 助手完成回复', 'expression'),
  'assistant-sleepy': asset('/mascot/current/expressions/sleepy.png', 'AnimateTool 助手暂时待机', 'expression'),
}

export const mascotFor = (scene: MascotScene): MascotAsset => scenes[scene]

export const mascotScenes = scenes
