import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SetupView } from "./SetupView";

describe("SetupView", () => {
  it("renders the welcome heading", () => {
    render(<SetupView onSubmit={vi.fn()} />);
    expect(screen.getByRole("heading", { name: /welcome to muesli/i })).toBeInTheDocument();
  });

  it("renders the account-creation description", () => {
    render(<SetupView onSubmit={vi.fn()} />);
    expect(screen.getByText(/create your operator account to get started/i)).toBeInTheDocument();
  });

  it("submits email and password to onSubmit", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<SetupView onSubmit={onSubmit} />);

    await userEvent.type(screen.getByLabelText(/email/i), "owner@example.com");
    await userEvent.type(screen.getByLabelText(/password/i), "password123");
    await userEvent.click(screen.getByRole("button", { name: /create account/i }));

    expect(onSubmit).toHaveBeenCalledWith("owner@example.com", "password123");
  });

  it("shows an error message when onSubmit rejects", async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error("account already exists"));
    render(<SetupView onSubmit={onSubmit} />);

    await userEvent.type(screen.getByLabelText(/email/i), "o@x.com");
    await userEvent.type(screen.getByLabelText(/password/i), "password123");
    await userEvent.click(screen.getByRole("button", { name: /create account/i }));

    expect(await screen.findByText(/account already exists/i)).toBeInTheDocument();
  });

  it("disables submit until both fields are filled", async () => {
    render(<SetupView onSubmit={vi.fn()} />);
    const button = screen.getByRole("button", { name: /create account/i });
    expect(button).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/email/i), "o@x.com");
    await userEvent.type(screen.getByLabelText(/password/i), "password123");
    expect(button).toBeEnabled();
  });
});
