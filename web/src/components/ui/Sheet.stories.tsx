import type { Meta, StoryObj } from "@storybook/react-vite";

import { Button } from "./Button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "./Sheet";

const meta: Meta<typeof Sheet> = {
  title: "UI / Sheet",
  component: Sheet,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "Slide-in side panel primitive wrapping `@radix-ui/react-dialog`. Four sides available (right, left, top, bottom). Use for Issue detail panels, agent settings, or any contextual side-pane workflow without leaving the chat surface.",
      },
    },
  },
  argTypes: {},
};

export default meta;

type Story = StoryObj<typeof Sheet>;

export const Right: Story = {
  render: () => (
    <Sheet>
      <SheetTrigger asChild={true}>
        <Button>Open from right</Button>
      </SheetTrigger>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>Issue NEX-312</SheetTitle>
          <SheetDescription>
            New-client invoicing kickoff playbook. This is the side-pane preview
            operators will see when they click an Issue card in chat.
          </SheetDescription>
        </SheetHeader>
        <div style={{ marginTop: 16 }}>
          <p style={{ fontSize: 13, color: "var(--text-secondary, #888)" }}>
            Owner: @bookkeeper · State: review · Budget: $1.20 of $5
          </p>
        </div>
        <SheetFooter>
          <Button variant="outline">Request changes</Button>
          <Button>Approve</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  ),
};

export const Left: Story = {
  render: () => (
    <Sheet>
      <SheetTrigger asChild={true}>
        <Button variant="outline">Open from left</Button>
      </SheetTrigger>
      <SheetContent side="left">
        <SheetHeader>
          <SheetTitle>Quick filters</SheetTitle>
          <SheetDescription>
            Left-side panels are useful for filter / sort / navigation aids that
            should not push the main canvas out of view.
          </SheetDescription>
        </SheetHeader>
      </SheetContent>
    </Sheet>
  ),
};

export const Bottom: Story = {
  render: () => (
    <Sheet>
      <SheetTrigger asChild={true}>
        <Button variant="secondary">Open from bottom</Button>
      </SheetTrigger>
      <SheetContent side="bottom">
        <SheetHeader>
          <SheetTitle>Activity stream</SheetTitle>
          <SheetDescription>
            Bottom sheets suit transient activity peeks or message composers you
            want to overlay without leaving the current view.
          </SheetDescription>
        </SheetHeader>
      </SheetContent>
    </Sheet>
  ),
};
