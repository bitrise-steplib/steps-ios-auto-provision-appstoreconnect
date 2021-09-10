require 'spaceship'

def get_app(bundle_id)
  app = nil
  run_or_raise_preferred_error_message do
    app = Spaceship::Portal.app.find(bundle_id)
  end

  {
    id: app.app_id,
    bundleID: app.bundle_id
  }
end


# name = "Bitrise - (#{bundle_id.tr('.', ' ')})"
# Log.debug("registering app: #{name} with bundle id: (#{bundle_id})")

# app = nil
# run_or_raise_preferred_error_message { app = Spaceship::Portal.app.create!(bundle_id: bundle_id, name: name) }

# raise "failed to create app with bundle id: #{bundle_id}" unless app
# app