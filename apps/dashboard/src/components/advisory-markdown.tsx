import ReactMarkdown from "react-markdown"

// Renders vendor advisory text. react-markdown escapes raw HTML by default
// (we deliberately do not enable rehype-raw), so update text from remote
// repositories can never inject markup.
export function AdvisoryMarkdown({ text }: { text: string }) {
  return (
    <div className="space-y-2 text-sm leading-relaxed break-words [&_a]:text-primary [&_a]:underline [&_a]:underline-offset-2 [&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_li]:ml-4 [&_li]:list-disc [&_p]:m-0">
      <ReactMarkdown
        components={{
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noopener noreferrer nofollow">
              {children}
            </a>
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
