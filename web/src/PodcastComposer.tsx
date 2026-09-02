import { useState } from "react";
import { Headphones, Plus, Trash2 } from "lucide-react";
import { ErrorBox, Field } from "./ui";
import type { ImportSeed } from "./Workshop";

export function PodcastComposer({
  importSource,
}: {
  importSource: (seed: ImportSeed) => void;
}) {
  const [title, setTitle] = useState(""),
    [description, setDescription] = useState(""),
    [link, setLink] = useState(""),
    [image, setImage] = useState("");
  const [episodes, setEpisodes] = useState([
    { title: "", url: "", type: "audio/mpeg", length: 0, description: "" },
  ]);
  const [error, setError] = useState("");
  function preview() {
    setError("");
    if (!title || episodes.some((e) => !e.title || !e.url)) {
      setError("请填写播客名称、每集标题和音频地址。");
      return;
    }
    importSource({
      name: title,
      protocol: "podcast",
      content: JSON.stringify(
        {
          schema: "shadow.podcast/v1",
          title,
          description,
          link,
          image,
          episodes,
        },
        null,
        2,
      ),
    });
  }
  return (
    <>
      <section className="workshop-intro">
        <Headphones size={26} />
        <div>
          <h2>把自己的音频整理成播客</h2>
          <p>
            填写已有音频的 HTTP 地址，Relay 生成标准 RSS。也可直接导入
            folder2podcast 的 RSS，将本地目录扫描留给它处理。
          </p>
        </div>
      </section>
      <section className="panel workshop-card">
        <div className="form-grid">
          <Field label="播客名称">
            <input value={title} onChange={(e) => setTitle(e.target.value)} />
          </Field>
          <Field label="主页 URL（可选）">
            <input
              value={link}
              onChange={(e) => setLink(e.target.value)}
              placeholder="https://example.com/audio"
            />
          </Field>
          <Field label="简介">
            <input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
          <Field label="封面 URL（可选）">
            <input value={image} onChange={(e) => setImage(e.target.value)} />
          </Field>
        </div>
        <div className="section-title">
          <h3>节目清单</h3>
          <button
            className="secondary"
            onClick={() =>
              setEpisodes([
                ...episodes,
                {
                  title: "",
                  url: "",
                  type: "audio/mpeg",
                  length: 0,
                  description: "",
                },
              ])
            }
          >
            <Plus size={15} />
            添加一集
          </button>
        </div>
        {episodes.map((episode, index) => (
          <div className="compatibility-entry" key={index}>
            <div className="form-grid">
              {(
                [
                  ["title", "节目标题"],
                  ["url", "音频 URL"],
                  ["type", "音频 MIME 类型"],
                  ["length", "文件字节数（未知填 0）"],
                ] as const
              ).map(([key, caption]) => (
                <Field key={key} label={`${index + 1}. ${caption}`}>
                  <input
                    type={key === "length" ? "number" : "text"}
                    min={key === "length" ? 0 : undefined}
                    value={episode[key]}
                    onChange={(e) =>
                      setEpisodes(
                        episodes.map((v, i) =>
                          i === index
                            ? {
                                ...v,
                                [key]:
                                  key === "length"
                                    ? Number(e.target.value)
                                    : e.target.value,
                              }
                            : v,
                        ),
                      )
                    }
                  />
                </Field>
              ))}
            </div>
            <button
              className="text-button"
              disabled={episodes.length === 1}
              onClick={() =>
                setEpisodes(episodes.filter((_, i) => i !== index))
              }
            >
              <Trash2 size={14} />
              移除此集
            </button>
          </div>
        ))}
        <button className="primary" onClick={preview}>
          预览并导入播客
        </button>
        <p className="muted">
          批准并加入编排组后，podcasts/feed.xml
          是合并的播客订阅；发布不会复制或代理音频文件。
        </p>
        <ErrorBox error={error} />
      </section>
    </>
  );
}
