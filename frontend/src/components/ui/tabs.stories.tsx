import type { Meta, StoryObj } from '@storybook/react-vite'
import { Users, KeyRound, History } from 'lucide-react'
import { Tabs, TabsList, TabsTrigger, TabsContent } from './tabs'

const meta: Meta<typeof Tabs> = {
  title: 'UI/Tabs',
  component: Tabs,
}
export default meta

type Story = StoryObj<typeof Tabs>

export const Default: Story = {
  render: () => (
    <Tabs defaultValue="members" className="w-[28rem]">
      <TabsList>
        <TabsTrigger value="members">Members</TabsTrigger>
        <TabsTrigger value="groups">Groups</TabsTrigger>
        <TabsTrigger value="sso">Single sign-on</TabsTrigger>
        <TabsTrigger value="activity">Activity</TabsTrigger>
      </TabsList>
      <TabsContent value="members" className="pt-[var(--space-4)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        Manage who belongs to the organization.
      </TabsContent>
      <TabsContent value="groups" className="pt-[var(--space-4)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        Sub-teams within the org.
      </TabsContent>
      <TabsContent value="sso" className="pt-[var(--space-4)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        Connect an OIDC identity provider.
      </TabsContent>
      <TabsContent value="activity" className="pt-[var(--space-4)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        Recent access changes.
      </TabsContent>
    </Tabs>
  ),
}

export const WithIcons: Story = {
  render: () => (
    <Tabs defaultValue="members" className="w-[28rem]">
      <TabsList>
        <TabsTrigger value="members">
          <Users width={14} height={14} />
          Members
        </TabsTrigger>
        <TabsTrigger value="sso">
          <KeyRound width={14} height={14} />
          Single sign-on
        </TabsTrigger>
        <TabsTrigger value="activity">
          <History width={14} height={14} />
          Activity
        </TabsTrigger>
      </TabsList>
      <TabsContent value="members" className="pt-[var(--space-4)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        Members panel.
      </TabsContent>
      <TabsContent value="sso" className="pt-[var(--space-4)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        SSO panel.
      </TabsContent>
      <TabsContent value="activity" className="pt-[var(--space-4)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        Activity panel.
      </TabsContent>
    </Tabs>
  ),
}
