export function updateConversationStreamingOwner(
  owners: Map<string, Set<string>>,
  publicID: string,
  ownerID: string,
  streaming: boolean,
): boolean {
  if (streaming) {
    const current = owners.get(publicID) ?? new Set<string>();
    current.add(ownerID);
    owners.set(publicID, current);
    return true;
  }

  const current = owners.get(publicID);
  current?.delete(ownerID);
  if (current?.size === 0) {
    owners.delete(publicID);
  }
  return owners.has(publicID);
}
