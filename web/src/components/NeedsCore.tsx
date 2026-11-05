/** A choice that only works through Pocket Core, on a box without one: what it needs, next to the control. */
export function NeedsCore({ what }: { what?: string }) {
  return (
    <p className="needs-core">{what ? `${what} needs Pocket Core.` : 'Needs Pocket Core.'} Pocket Core connects your Pocket apps to each other and to your services: one sign-in for everyone, each person's own accounts, nothing to copy or paste. Set CORE_URL to connect this app. <a href="https://github.com/abjelosevic88/pocket-homelab-core" target="_blank" rel="noreferrer">Learn more</a></p>
  )
}
