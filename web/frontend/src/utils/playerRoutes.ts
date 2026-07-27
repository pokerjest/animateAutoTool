export const localPlayerPath = '/media/local-player'

export function localPlayerLocation(animeID: number | string, episodeID?: number | string, autoplay = false) {
  const query: Record<string, string> = { anime: String(animeID) }
  if (episodeID !== undefined && episodeID !== null && String(episodeID)) query.episode = String(episodeID)
  if (autoplay) query.autoplay = '1'
  return { path: localPlayerPath, query }
}
