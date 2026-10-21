// The remove button is shown only where the server will accept the removal.
// An admin cannot remove the owner, and nobody removes themselves from this list.
export function canRemoveMember(
  member: { UserID: number; Role: string },
  viewer: { ID: number },
  viewerIsOwner: boolean,
): boolean {
  if (member.UserID === viewer.ID) return false
  return !(member.Role === "owner" && !viewerIsOwner)
}
